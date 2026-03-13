package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	PanelURL        string
	PanelUsername   string
	PanelPassword   string
	ListenAddr      string
	OnlineThreshold time.Duration
	ScrapeTimeout   time.Duration

	PanelBasicUser string
	PanelBasicPass string
}

func LoadFromEnv() (*Config, error) {
	// Read required environment variables
	panelURL := os.Getenv("PANEL_URL")
	if panelURL == "" {
		return nil, fmt.Errorf("PANEL_URL is required")
	}

	panelUsername := os.Getenv("PANEL_USERNAME")
	if panelUsername == "" {
		return nil, fmt.Errorf("PANEL_USERNAME is required")
	}

	panelPassword := os.Getenv("PANEL_PASSWORD")
	if panelPassword == "" {
		return nil, fmt.Errorf("PANEL_PASSWORD is required")
	}

	// Read optional environment variables with defaults
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":9115"
	}

	onlineThresholdStr := os.Getenv("ONLINE_THRESHOLD")
	if onlineThresholdStr == "" {
		onlineThresholdStr = "2m"
	}
	onlineThreshold, err := time.ParseDuration(onlineThresholdStr)
	if err != nil {
		return nil, fmt.Errorf("invalid ONLINE_THRESHOLD: %w", err)
	}

	scrapeTimeoutStr := os.Getenv("SCRAPE_TIMEOUT")
	if scrapeTimeoutStr == "" {
		scrapeTimeoutStr = "30s"
	}
	scrapeTimeout, err := time.ParseDuration(scrapeTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid SCRAPE_TIMEOUT: %w", err)
	}

	return &Config{
		PanelURL:        panelURL,
		PanelUsername:   panelUsername,
		PanelPassword:   panelPassword,
		ListenAddr:      listenAddr,
		OnlineThreshold: onlineThreshold,
		ScrapeTimeout:   scrapeTimeout,
		PanelBasicUser:  os.Getenv("PANEL_BASIC_AUTH_USERNAME"),
		PanelBasicPass:  os.Getenv("PANEL_BASIC_AUTH_PASSWORD"),
	}, nil
}
