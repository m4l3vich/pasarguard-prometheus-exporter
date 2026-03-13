package node

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"google.golang.org/protobuf/proto"

	statpb "github.com/pasarguard/pasarguard-exporter/internal/node/proto"
)

// NodeEndpoint holds connection info for one PasarGuard Node.
type NodeEndpoint struct {
	Address  string // "host:api_port" e.g. "1.2.3.4:62050"
	APIKey   string // UUID authentication key
	ServerCA string // PEM-encoded CA certificate (may be empty)
}

// UserStat holds per-user traffic stats aggregated from one node.
type UserStat struct {
	Email    string // PasarGuard username (from Xray stat name)
	Upload   int64  // bytes uploaded (Xray "uplink")
	Download int64  // bytes downloaded (Xray "downlink")
}

// Client fetches stats from PasarGuard Node REST API using protobuf encoding.
type Client struct{}

// NewClient creates a new Node REST client.
func NewClient() *Client {
	return &Client{}
}

// GetStats fetches per-user traffic stats from a single node.
// Returns empty slice (not error) if node has no stats.
// Returns error on network failure, TLS error, or protobuf parse failure.
func (c *Client) GetStats(ctx context.Context, endpoint NodeEndpoint) ([]UserStat, error) {
	// Build TLS config.
	tlsCfg := &tls.Config{}
	if endpoint.ServerCA != "" {
		pool := x509.NewCertPool()
		if ok := pool.AppendCertsFromPEM([]byte(endpoint.ServerCA)); !ok {
			return nil, fmt.Errorf("failed to parse ServerCA certificate")
		}
		tlsCfg.RootCAs = pool
	} else {
		tlsCfg.InsecureSkipVerify = true // no CA provided — skip verification
	}

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
		// No timeout — caller controls deadline via ctx.
	}

	// Build protobuf request: Reset_=false is CRITICAL — never steal Panel's data.
	reqMsg := &statpb.StatRequest{
		Name:   "",                        // empty = all stats
		Reset_: false,                     // NEVER reset counters
		Type:   statpb.StatType_UsersStat, // fetch per-user stats
	}
	body, err := proto.Marshal(reqMsg)
	if err != nil {
		return nil, fmt.Errorf("marshal StatRequest: %w", err)
	}

	// Build HTTP request: GET /stats/ with protobuf body.
	url := fmt.Sprintf("https://%s/stats/", endpoint.Address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("x-api-key", endpoint.APIKey)

	// Execute request.
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", endpoint.Address, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", endpoint.Address, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("node %s returned HTTP %d: %s", endpoint.Address, resp.StatusCode, string(respBytes))
	}

	// Parse protobuf response.
	var statResp statpb.StatResponse
	if err := proto.Unmarshal(respBytes, &statResp); err != nil {
		return nil, fmt.Errorf("parse response from %s: %w", endpoint.Address, err)
	}

	// Parse stat names and build per-user aggregates.
	return parseUserStats(statResp.GetStats()), nil
}

// parseUserStats extracts per-user traffic from Xray stat entries.
//
// Xray stat name format: "user>>>{email}>>>traffic>>>uplink" or "user>>>{email}>>>traffic>>>downlink"
// Non-user stats (e.g. inbound/outbound) are skipped.
func parseUserStats(stats []*statpb.Stat) []UserStat {
	byEmail := make(map[string]*UserStat)

	for _, s := range stats {
		name := s.GetName()
		if name == "" {
			continue
		}

		parts := strings.Split(name, ">>>")
		if len(parts) != 4 {
			continue
		}
		if parts[0] != "user" {
			continue
		}

		email := cleanEmail(parts[1])
		direction := parts[3] // "uplink" or "downlink"

		us, ok := byEmail[email]
		if !ok {
			us = &UserStat{Email: email}
			byEmail[email] = us
		}

		switch direction {
		case "uplink":
			us.Upload += s.GetValue()
		case "downlink":
			us.Download += s.GetValue()
		}
	}

	result := make([]UserStat, 0, len(byEmail))
	for _, us := range byEmail {
		result = append(result, *us)
	}
	return result
}

// cleanEmail strips the numeric ID prefix from Xray email format.
// Xray may emit email as "{id}.{username}" where id is numeric.
// If it contains a dot and the part before the first dot is all digits,
// strip the prefix and return the rest. Otherwise return as-is.
func cleanEmail(raw string) string {
	dotIdx := strings.IndexByte(raw, '.')
	if dotIdx < 1 {
		return raw
	}

	prefix := raw[:dotIdx]
	allDigits := true
	for _, r := range prefix {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}

	if allDigits {
		return raw[dotIdx+1:]
	}
	return raw
}
