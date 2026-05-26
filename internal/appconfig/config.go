package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version      int          `json:"version" yaml:"version"`
	Workspace    string       `json:"workspace" yaml:"workspace"`
	Environment  string       `json:"environment" yaml:"environment"`
	AuthToken    string       `json:"authToken" yaml:"authToken"`
	Notification Notification `json:"notification" yaml:"notification"`
	Proxy        ProxyConfig  `json:"proxy" yaml:"proxy"`
}

type Notification struct {
	WebhookURLs          []string `json:"webhookUrls" yaml:"webhookUrls"`
	SlackWebhookURLs     []string `json:"slackWebhookUrls" yaml:"slackWebhookUrls"`
	PagerDutyRoutingKeys []string `json:"pagerDutyRoutingKeys" yaml:"pagerDutyRoutingKeys"`
	RetryMaxAttempts     int      `json:"retryMaxAttempts" yaml:"retryMaxAttempts"`
	RetryBackoffSeconds  int      `json:"retryBackoffSeconds" yaml:"retryBackoffSeconds"`
}

type ProxyConfig struct {
	DB         string   `json:"db" yaml:"db"`
	Store      string   `json:"store" yaml:"store"`
	DSN        string   `json:"dsn" yaml:"dsn"`
	Port       int      `json:"port" yaml:"port"`
	Transport  string   `json:"transport" yaml:"transport"`
	RetainFor  string   `json:"retainFor" yaml:"retainFor"`
	Shutdown   string   `json:"shutdownTimeout" yaml:"shutdownTimeout"`
	MaxTraces  int      `json:"maxTraces" yaml:"maxTraces"`
	RedactKeys []string `json:"redactKeys" yaml:"redactKeys"`
	EnableOTEL bool     `json:"otel" yaml:"otel"`
}

func Load(path string) (Config, error) {
	resolved := strings.TrimSpace(path)
	if resolved == "" {
		resolved = findDefaultConfigPath()
	}
	if resolved == "" {
		return Config{Version: 1}, nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
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
	if strings.TrimSpace(c.Proxy.Store) != "" {
		switch strings.ToLower(strings.TrimSpace(c.Proxy.Store)) {
		case "sqlite", "postgres":
		default:
			return fmt.Errorf("proxy.store must be sqlite or postgres")
		}
	}
	if strings.EqualFold(strings.TrimSpace(c.Proxy.Store), "postgres") && strings.TrimSpace(c.Proxy.DSN) == "" {
		return fmt.Errorf("proxy.dsn is required when proxy.store=postgres")
	}
	if strings.TrimSpace(c.Proxy.RetainFor) != "" {
		duration, err := time.ParseDuration(c.Proxy.RetainFor)
		if err != nil {
			return fmt.Errorf("proxy.retainFor must be a valid duration")
		}
		if duration < 0 {
			return fmt.Errorf("proxy.retainFor must be non-negative")
		}
	}
	if strings.TrimSpace(c.Proxy.Shutdown) != "" {
		duration, err := time.ParseDuration(c.Proxy.Shutdown)
		if err != nil {
			return fmt.Errorf("proxy.shutdownTimeout must be a valid duration")
		}
		if duration <= 0 {
			return fmt.Errorf("proxy.shutdownTimeout must be greater than 0")
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

func findDefaultConfigPath() string {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "mcpscope.yaml"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, ".config", "mcpscope", "config.yaml"))
	}
	candidates = append(candidates, filepath.Join(string(os.PathSeparator), "etc", "mcpscope", "config.yaml"))

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
