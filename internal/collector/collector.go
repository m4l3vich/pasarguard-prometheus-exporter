package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/pasarguard/pasarguard-exporter/internal/node"
	"github.com/pasarguard/pasarguard-exporter/internal/panel"
)

// accumKey uniquely identifies a (user, direction, node) accumulator.
type accumKey struct {
	Email     string
	Direction string // "upload" or "download"
	NodeAddr  string // node address (to handle per-node Xray resets independently)
}

var (
	downBytesDesc      = prometheus.NewDesc("down_bytes_total", "Bytes sent to the peer", []string{"email"}, nil)
	upBytesDesc        = prometheus.NewDesc("up_bytes_total", "Bytes received from the peer", []string{"email"}, nil)
	isOnlineDesc       = prometheus.NewDesc("is_online", "Is the peer online", []string{"email"}, nil)
	scrapeUpDesc       = prometheus.NewDesc("pasarguard_up", "Whether the last scrape was successful", nil, nil)
	scrapeDurationDesc = prometheus.NewDesc("pasarguard_scrape_duration_seconds", "Duration of the last scrape in seconds", nil, nil)
)

// Collector implements prometheus.Collector for PasarGuard metrics.
// It queries Panel and Node APIs on every Prometheus scrape, handles
// the Xray counter reset problem with an in-memory accumulator, and
// emits the correct metric types.
type Collector struct {
	panelClient     *panel.Client
	nodeClient      *node.Client
	onlineThreshold time.Duration
	scrapeTimeout   time.Duration

	mu           sync.Mutex
	accumulators map[accumKey]int64 // monotonically increasing totals
	lastSeen     map[accumKey]int64 // last raw value from Xray
}

// NewCollector creates a new Collector.
func NewCollector(panelClient *panel.Client, nodeClient *node.Client, onlineThreshold, scrapeTimeout time.Duration) *Collector {
	return &Collector{
		panelClient:     panelClient,
		nodeClient:      nodeClient,
		onlineThreshold: onlineThreshold,
		scrapeTimeout:   scrapeTimeout,
		accumulators:    make(map[accumKey]int64),
		lastSeen:        make(map[accumKey]int64),
	}
}

// Describe sends metric descriptors to the channel.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- downBytesDesc
	ch <- upBytesDesc
	ch <- isOnlineDesc
	ch <- scrapeUpDesc
	ch <- scrapeDurationDesc
}

// Collect performs a synchronous scrape: queries Panel for users/nodes,
// queries each node for traffic stats, updates accumulators, and emits metrics.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), c.scrapeTimeout)
	defer cancel()

	// 1. Get user list from Panel (authoritative source of usernames + online status).
	users, err := c.panelClient.GetUsers(ctx)
	if err != nil {
		slog.Error("failed to get users from panel", "error", err)
		ch <- prometheus.MustNewConstMetric(scrapeUpDesc, prometheus.GaugeValue, 0)
		ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds())
		return
	}

	// 2. Get node list from Panel.
	nodes, err := c.panelClient.GetNodes(ctx)
	if err != nil {
		slog.Error("failed to get nodes from panel", "error", err)
		ch <- prometheus.MustNewConstMetric(scrapeUpDesc, prometheus.GaugeValue, 0)
		ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds())
		return
	}

	// 3. Collect per-user stats from each node.
	// rawByNode[nodeAddr] = []UserStat — each node's Xray resets independently.
	rawByNode := make(map[string][]node.UserStat)
	for _, n := range nodes {
		if n.Status != "connected" {
			continue
		}
		endpoint := node.NodeEndpoint{
			Address:  fmt.Sprintf("%s:%d", n.Address, n.Port),
			APIKey:   n.APIKey,
			ServerCA: n.ServerCA,
		}
		stats, err := c.nodeClient.GetStats(ctx, endpoint)
		if err != nil {
			slog.Warn("failed to get stats from node", "node", n.Name, "addr", endpoint.Address, "error", err)
			continue // skip unreachable nodes
		}
		rawByNode[n.Address] = stats
	}

	// 4. Update accumulators and compute per-user totals.
	c.mu.Lock()

	type emailTotals struct{ upload, download int64 }
	totals := make(map[string]*emailTotals)

	for nodeAddr, stats := range rawByNode {
		for _, s := range stats {
			uploadKey := accumKey{Email: s.Email, Direction: "upload", NodeAddr: nodeAddr}
			c.updateAccum(uploadKey, s.Upload)

			downloadKey := accumKey{Email: s.Email, Direction: "download", NodeAddr: nodeAddr}
			c.updateAccum(downloadKey, s.Download)

			et := totals[s.Email]
			if et == nil {
				et = &emailTotals{}
				totals[s.Email] = et
			}
			et.upload += c.accumulators[uploadKey]
			et.download += c.accumulators[downloadKey]
		}
	}

	c.mu.Unlock()

	// 5. Emit metrics for each user from Panel (Panel is authoritative source).
	for _, u := range users {
		email := u.Username

		var uploadTotal int64
		if et, ok := totals[email]; ok {
			uploadTotal = et.upload
		}
		ch <- prometheus.MustNewConstMetric(upBytesDesc, prometheus.CounterValue, float64(uploadTotal), email)

		var downloadTotal int64
		if et, ok := totals[email]; ok {
			downloadTotal = et.download
		}
		ch <- prometheus.MustNewConstMetric(downBytesDesc, prometheus.CounterValue, float64(downloadTotal), email)

		isOnline := 0.0
		if u.OnlineAt != nil && time.Since(*u.OnlineAt) < c.onlineThreshold {
			isOnline = 1.0
		}
		ch <- prometheus.MustNewConstMetric(isOnlineDesc, prometheus.GaugeValue, isOnline, email)
	}

	ch <- prometheus.MustNewConstMetric(scrapeUpDesc, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds())
}

// updateAccum updates the accumulator for a key given a new raw value from Xray.
// MUST be called with c.mu held.
//
// Handles the Xray counter reset problem:
//   - Panel periodically reads nodes with reset=true, zeroing Xray counters
//   - Our exporter reads with reset=false, observing raw values
//   - When rawValue < lastSeen, a Panel reset occurred
//   - rawValue after a reset represents new traffic since the reset
//   - We add it to our accumulator so accumulators are monotonically increasing
func (c *Collector) updateAccum(key accumKey, rawValue int64) {
	prev, exists := c.lastSeen[key]
	if !exists {
		// First time seeing this key — initialize.
		c.accumulators[key] = rawValue
		c.lastSeen[key] = rawValue
		return
	}

	if rawValue >= prev {
		// Normal case: Xray counter grew — add the delta.
		delta := rawValue - prev
		c.accumulators[key] += delta
	} else {
		// rawValue < prev: Panel reset Xray counters.
		// rawValue is the new accumulation since the reset.
		c.accumulators[key] += rawValue
	}
	c.lastSeen[key] = rawValue
}
