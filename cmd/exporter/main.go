package main

import (
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
)

func main() {
	// 1. Load config from env
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// 2. Create clients
	panelClient := panel.NewClient(cfg.PanelURL, cfg.PanelUsername, cfg.PanelPassword)
	nodeClient := node.NewClient()

	// 3. Create collector
	coll := collector.NewCollector(panelClient, nodeClient, cfg.OnlineThreshold, cfg.ScrapeTimeout)

	// 4. Create CUSTOM Prometheus registry (NOT default — avoids Go process metrics)
	reg := prometheus.NewRegistry()
	reg.MustRegister(coll)

	// 5. Create HTTP mux with /metrics handler
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	// 6. Start server
	slog.Info("pasarguard-exporter starting", "addr", cfg.ListenAddr)

	// 7. Signal handling (minimal — log and exit)
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
