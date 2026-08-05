package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	PanelURL        string
	PanelUsername   string
	PanelPassword   string
	PanelAPIKey     string
	ListenAddr      string
	OnlineThreshold time.Duration
	ScrapeTimeout   time.Duration

	PanelBasicUser string
	PanelBasicPass string

	PanelTLSCert string
	PanelTLSKey  string
	PanelTLSCA   string

	NodeTLSCert string
	NodeTLSKey  string
}

// readSecret returns the value of valueEnv if set, otherwise reads and
// trims the contents of the file named by fileEnv. Returns "" if neither is set.
func readSecret(valueEnv, fileEnv string) (string, error) {
	if v := os.Getenv(valueEnv); v != "" {
		return v, nil
	}
	path := os.Getenv(fileEnv)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fileEnv, err)
	}
	return strings.TrimRight(string(data), "\n\r"), nil
}

func LoadFromEnv() (*Config, error) {
	// Read required environment variables
	panelURL := os.Getenv("PANEL_URL")
	if panelURL == "" {
		return nil, fmt.Errorf("PANEL_URL is required")
	}

	panelAPIKey, err := readSecret("PANEL_API_KEY", "PANEL_API_KEY_FILE")
	if err != nil {
		return nil, err
	}

	panelUsername := os.Getenv("PANEL_USERNAME")
	panelPassword, err := readSecret("PANEL_PASSWORD", "PANEL_PASSWORD_FILE")
	if err != nil {
		return nil, err
	}

	// Either an API key or a username/password pair is required. The API key
	// takes precedence if both are configured.
	if panelAPIKey == "" {
		if panelUsername == "" {
			return nil, fmt.Errorf("PANEL_USERNAME is required (or set PANEL_API_KEY)")
		}
		if panelPassword == "" {
			return nil, fmt.Errorf("PANEL_PASSWORD or PANEL_PASSWORD_FILE is required (or set PANEL_API_KEY)")
		}
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
		PanelAPIKey:     panelAPIKey,
		ListenAddr:      listenAddr,
		OnlineThreshold: onlineThreshold,
		ScrapeTimeout:   scrapeTimeout,
		PanelBasicUser:  os.Getenv("PANEL_BASIC_AUTH_USERNAME"),
		PanelBasicPass:  os.Getenv("PANEL_BASIC_AUTH_PASSWORD"),
		PanelTLSCert:    os.Getenv("PANEL_TLS_CERT_FILE"),
		PanelTLSKey:     os.Getenv("PANEL_TLS_KEY_FILE"),
		PanelTLSCA:      os.Getenv("PANEL_TLS_CA_FILE"),
		NodeTLSCert:     os.Getenv("NODE_TLS_CERT_FILE"),
		NodeTLSKey:      os.Getenv("NODE_TLS_KEY_FILE"),
	}, nil
}
