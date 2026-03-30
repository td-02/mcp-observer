package appconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Version      int          `json:"version"`
	Workspace    string       `json:"workspace"`
	Environment  string       `json:"environment"`
	AuthToken    string       `json:"authToken"`
	Notification Notification `json:"notification"`
	Proxy        ProxyConfig  `json:"proxy"`
}

type Notification struct {
	WebhookURLs          []string `json:"webhookUrls"`
	SlackWebhookURLs     []string `json:"slackWebhookUrls"`
	PagerDutyRoutingKeys []string `json:"pagerDutyRoutingKeys"`
	RetryMaxAttempts     int      `json:"retryMaxAttempts"`
	RetryBackoffSeconds  int      `json:"retryBackoffSeconds"`
}

type ProxyConfig struct {
	DB         string   `json:"db"`
	Port       int      `json:"port"`
	Transport  string   `json:"transport"`
	RetainFor  string   `json:"retainFor"`
	MaxTraces  int      `json:"maxTraces"`
	RedactKeys []string `json:"redactKeys"`
	EnableOTEL bool     `json:"otel"`
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config file: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	if strings.TrimSpace(c.Proxy.Transport) != "" {
		switch strings.ToLower(strings.TrimSpace(c.Proxy.Transport)) {
		case "stdio", "http":
		default:
			return fmt.Errorf("proxy.transport must be stdio or http")
		}
	}
	if c.Proxy.Port < 0 || c.Proxy.Port > 65535 {
		return fmt.Errorf("proxy.port must be between 0 and 65535")
	}
	if c.Proxy.MaxTraces < 0 {
		return fmt.Errorf("proxy.maxTraces must be 0 or greater")
	}
	if strings.TrimSpace(c.Proxy.RetainFor) != "" {
		if _, err := time.ParseDuration(c.Proxy.RetainFor); err != nil {
			return fmt.Errorf("proxy.retainFor must be a valid duration")
		}
	}
	if c.Notification.RetryMaxAttempts < 0 {
		return fmt.Errorf("notification.retryMaxAttempts must be 0 or greater")
	}
	if c.Notification.RetryBackoffSeconds < 0 {
		return fmt.Errorf("notification.retryBackoffSeconds must be 0 or greater")
	}
	return nil
}

func (c Config) RetentionDuration() time.Duration {
	if strings.TrimSpace(c.Proxy.RetainFor) == "" {
		return 0
	}
	duration, err := time.ParseDuration(c.Proxy.RetainFor)
	if err != nil {
		return 0
	}
	return duration
}
