package node

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"unicode"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	statpb "github.com/pasarguard/pasarguard-exporter/internal/node/proto"
)

// NodeEndpoint holds connection info for one PasarGuard Node.
type NodeEndpoint struct {
	Address  string // "host:port" e.g. "1.2.3.4:62050"
	APIKey   string // UUID authentication key
	ServerCA string // PEM-encoded CA certificate (may be empty)
}

// UserStat holds per-user traffic stats aggregated from one node.
type UserStat struct {
	Email    string // PasarGuard username (from Xray stat name)
	Upload   int64  // bytes uploaded (Xray "uplink")
	Download int64  // bytes downloaded (Xray "downlink")
}

// Client fetches stats from PasarGuard Node gRPC API.
type Client struct {
	clientCert *tls.Certificate
}

// NewClient creates a new Node gRPC client.
// clientCert may be nil if mTLS is not needed.
func NewClient(clientCert *tls.Certificate) *Client {
	return &Client{clientCert: clientCert}
}

// GetStats fetches per-user traffic stats from a single node via gRPC.
func (c *Client) GetStats(ctx context.Context, endpoint NodeEndpoint) ([]UserStat, error) {
	creds, err := c.buildTransportCredentials(endpoint)
	if err != nil {
		return nil, fmt.Errorf("build TLS for %s: %w", endpoint.Address, err)
	}

	conn, err := grpc.NewClient(endpoint.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", endpoint.Address, err)
	}
	defer conn.Close()

	client := statpb.NewNodeServiceClient(conn)

	req := &statpb.StatRequest{
		Name:   "",
		Reset_: false,
		Type:   statpb.StatType_UsersStat,
	}

	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", endpoint.APIKey)

	resp, err := client.GetStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetStats from %s: %w", endpoint.Address, err)
	}

	return parseUserStats(resp.GetStats()), nil
}

func (c *Client) buildTransportCredentials(endpoint NodeEndpoint) (credentials.TransportCredentials, error) {
	if endpoint.ServerCA == "" && c.clientCert == nil {
		return insecure.NewCredentials(), nil
	}

	tlsCfg := &tls.Config{}

	if endpoint.ServerCA != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(endpoint.ServerCA)) {
			return nil, fmt.Errorf("failed to parse ServerCA certificate")
		}
		tlsCfg.RootCAs = pool
	} else {
		tlsCfg.InsecureSkipVerify = true
	}

	if c.clientCert != nil {
		tlsCfg.Certificates = []tls.Certificate{*c.clientCert}
	}

	return credentials.NewTLS(tlsCfg), nil
}

// parseUserStats extracts per-user traffic from Node gRPC Stat entries.
//
// The Node's GetUsersStats RPC transforms raw Xray stat names before returning them.
// Raw Xray format: "user>>>{email}>>>traffic>>>uplink"
// Transformed gRPC Stat fields:
//
//	Name = email (e.g. "1.alice")
//	Type = direction ("uplink" or "downlink")
//	Link = "traffic"
//	Value = bytes
func parseUserStats(stats []*statpb.Stat) []UserStat {
	byEmail := make(map[string]*UserStat)

	for _, s := range stats {
		email := cleanEmail(s.GetName())
		if email == "" {
			continue
		}

		direction := s.GetType() // "uplink" or "downlink"

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
