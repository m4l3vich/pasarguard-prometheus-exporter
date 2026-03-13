# PasarGuard Prometheus Exporter

A lightweight Prometheus exporter for [PasarGuard](https://github.com/PasarGuard) that exposes per-user traffic counters and online status as Prometheus metrics.

It queries the PasarGuard **Panel API** for authentication, user lists, node discovery, and online status, and each **Node REST API** for per-user upload/download byte counters via Xray stats.

## Metrics

```
# HELP down_bytes_total Bytes sent to the peer
# TYPE down_bytes_total counter
down_bytes_total{email="alice"} 982372615

# HELP up_bytes_total Bytes received from the peer
# TYPE up_bytes_total counter
up_bytes_total{email="alice"} 52819433

# HELP is_online Is the peer online
# TYPE is_online gauge
is_online{email="alice"} 1

# HELP pasarguard_up Whether the last scrape was successful
# TYPE pasarguard_up gauge
pasarguard_up 1

# HELP pasarguard_scrape_duration_seconds Duration of the last scrape in seconds
# TYPE pasarguard_scrape_duration_seconds gauge
pasarguard_scrape_duration_seconds 0.342
```

The `email` label contains the human-readable PasarGuard username (not a numeric ID).

## Configuration

All configuration is through environment variables. No config files.

| Variable | Required | Default | Description |
|---|---|---|---|
| `PANEL_URL` | **Yes** | — | PasarGuard Panel base URL (e.g. `https://panel.example.com`) |
| `PANEL_USERNAME` | **Yes** | — | Panel admin username |
| `PANEL_PASSWORD` | **Yes**\* | — | Panel admin password |
| `PANEL_PASSWORD_FILE` | **Yes**\* | — | Path to file containing the Panel admin password (alternative to `PANEL_PASSWORD`) |
| `LISTEN_ADDR` | No | `:9115` | Address and port the exporter listens on |
| `ONLINE_THRESHOLD` | No | `2m` | Duration since last `online_at` to consider a user online (Go duration format) |
| `SCRAPE_TIMEOUT` | No | `30s` | Maximum time for a single scrape to complete (Go duration format) |
| `PANEL_BASIC_AUTH_USERNAME` | No | — | HTTP Basic Auth username for Panel requests (e.g. reverse proxy auth) |
| `PANEL_BASIC_AUTH_PASSWORD` | No | — | HTTP Basic Auth password for Panel requests |
| `PANEL_TLS_CERT_FILE` | No | — | Path to client certificate PEM file for mTLS with the Panel |
| `PANEL_TLS_KEY_FILE` | No | — | Path to client private key PEM file for mTLS with the Panel |
| `PANEL_TLS_CA_FILE` | No | — | Path to CA certificate PEM file to verify the Panel's server certificate |
| `NODE_TLS_CERT_FILE` | No | — | Path to client certificate PEM file for mTLS with Nodes |
| `NODE_TLS_KEY_FILE` | No | — | Path to client private key PEM file for mTLS with Nodes |

\* One of `PANEL_PASSWORD` or `PANEL_PASSWORD_FILE` is required. `PANEL_PASSWORD` takes precedence if both are set. Trailing newlines are stripped from the file.

The Panel account must have **sudo admin** privileges (required to access node API keys and stats).

If your Panel is behind a reverse proxy that requires HTTP Basic Auth, set both `PANEL_BASIC_AUTH_USERNAME` and `PANEL_BASIC_AUTH_PASSWORD`. The credentials are sent as an `Authorization: Basic ...` header on every request to the Panel API, alongside the JWT Bearer token.

### mTLS (Mutual TLS)

To use client certificates for authentication with the Panel or Node APIs, set the corresponding `*_TLS_CERT_FILE` and `*_TLS_KEY_FILE` pairs. Panel and Node use independent cert pairs.

`PANEL_TLS_CA_FILE` allows specifying a custom CA to verify the Panel's server certificate (useful for self-signed certs). Node server CAs are provided per-node via the Panel API's `server_ca` field.

## Quick Start

### Docker (recommended)

```bash
docker build -t pasarguard-exporter .

docker run -d \
  --name pasarguard-exporter \
  -p 9115:9115 \
  -e PANEL_URL=https://panel.example.com \
  -e PANEL_USERNAME=admin \
  -e PANEL_PASSWORD=secret \
  pasarguard-exporter
```

The image is ~18 MB (multi-stage build with [distroless](https://github.com/GoogleContainerTools/distroless)).

### Binary

Requires Go 1.23+.

```bash
go build -o pasarguard-exporter ./cmd/exporter/

export PANEL_URL=https://panel.example.com
export PANEL_USERNAME=admin
export PANEL_PASSWORD=secret

./pasarguard-exporter
```

Metrics are served at `http://localhost:9115/metrics`.

## Prometheus Configuration

Add a scrape job to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: pasarguard
    static_configs:
      - targets: ['localhost:9115']
    scrape_interval: 30s
    scrape_timeout: 30s
```

## How It Works

On each Prometheus scrape (`GET /metrics`):

1. **Authenticate** with the Panel API (JWT, auto-refreshes on 401).
2. **Fetch all users** from the Panel (paginated, 100 per page). The user list provides usernames and `online_at` timestamps.
3. **Discover nodes** from the Panel. Each node entry includes the `api_key` and `server_ca` needed to query it.
4. **Query each connected node** for per-user Xray traffic stats via its REST API (protobuf-encoded). Unreachable nodes are skipped — they don't fail the entire scrape.
5. **Accumulate counters** across scrapes. Because the Panel periodically resets Xray counters (`reset=true`), raw values can drop to zero at any time. The exporter reads with `reset=false` and maintains in-memory accumulators to produce monotonically increasing Prometheus counters.
6. **Emit metrics** for every user from the Panel's user list, with traffic totals summed across all nodes.

### Counter Accumulation

Xray's internal counters are reset to zero whenever the Panel reads them. The exporter handles this:

- If the raw value **grew** since last seen: add the delta to the accumulator.
- If the raw value **dropped** (Panel reset detected): treat the new value as fresh traffic since the reset and add it.

This ensures `up_bytes_total` and `down_bytes_total` are always monotonically increasing, which is what Prometheus expects from counters.

> **Note:** Accumulators are in-memory. Restarting the exporter resets counters to the current Xray values. This causes a one-time counter decrease visible in Prometheus as a counter reset — Prometheus handles this natively with its `rate()`/`increase()` functions.

### Online Status

A user is considered online (`is_online{email="..."} 1`) if their `online_at` timestamp from the Panel is within the `ONLINE_THRESHOLD` window (default: 2 minutes). Otherwise the value is `0`.

## Building

```bash
# Build
go build ./cmd/exporter/

# Vet
go vet ./...

# Docker
docker build -t pasarguard-exporter .
```