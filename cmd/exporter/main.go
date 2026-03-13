package main

import (
	"crypto/tls"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/pasarguard/pasarguard-exporter/internal/collector"
	"github.com/pasarguard/pasarguard-exporter/internal/config"
	"github.com/pasarguard/pasarguard-exporter/internal/node"
	"github.com/pasarguard/pasarguard-exporter/internal/panel"
	"github.com/pasarguard/pasarguard-exporter/internal/tlsutil"
)

func main() {
	// 1. Load config from env
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// 2. Build TLS configs
	var panelTLS *tls.Config
	if cfg.PanelTLSCert != "" && cfg.PanelTLSKey != "" {
		cert, err := tlsutil.LoadClientCert(cfg.PanelTLSCert, cfg.PanelTLSKey)
		if err != nil {
			log.Fatalf("panel TLS: %v", err)
		}
		panelTLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	if cfg.PanelTLSCA != "" {
		pool, err := tlsutil.LoadCAPool(cfg.PanelTLSCA)
		if err != nil {
			log.Fatalf("panel TLS CA: %v", err)
		}
		if panelTLS == nil {
			panelTLS = &tls.Config{}
		}
		panelTLS.RootCAs = pool
	}

	var nodeCert *tls.Certificate
	if cfg.NodeTLSCert != "" && cfg.NodeTLSKey != "" {
		cert, err := tlsutil.LoadClientCert(cfg.NodeTLSCert, cfg.NodeTLSKey)
		if err != nil {
			log.Fatalf("node TLS: %v", err)
		}
		nodeCert = &cert
	}

	// 3. Create clients
	panelClient := panel.NewClient(cfg.PanelURL, cfg.PanelUsername, cfg.PanelPassword, cfg.PanelBasicUser, cfg.PanelBasicPass, panelTLS)
	nodeClient := node.NewClient(nodeCert)

	// 4. Create collector
	coll := collector.NewCollector(panelClient, nodeClient, cfg.OnlineThreshold, cfg.ScrapeTimeout)

	// 5. Create CUSTOM Prometheus registry (NOT default — avoids Go process metrics)
	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)

	// 6. Create HTTP mux with /metrics handler
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	// 7. Start server
	slog.Info("pasarguard-exporter starting", "addr", cfg.ListenAddr)

	// 8. Signal handling (minimal — log and exit)
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		<-quit
		slog.Info("shutting down")
		os.Exit(0)
	}()

	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
