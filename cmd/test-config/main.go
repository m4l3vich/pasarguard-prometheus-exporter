package main

import (
	"fmt"
	"github.com/pasarguard/pasarguard-exporter/internal/config"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}
	fmt.Printf("OK: ListenAddr=%s OnlineThreshold=%s ScrapeTimeout=%s\n",
		cfg.ListenAddr, cfg.OnlineThreshold, cfg.ScrapeTimeout)
}
