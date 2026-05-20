package alerting

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Rules     []RuleConfig    `yaml:"rules" json:"rules"`
	Slack     SlackConfig     `yaml:"slack" json:"slack"`
	PagerDuty PagerDutyConfig `yaml:"pagerduty" json:"pagerduty"`
}

type RuleConfig struct {
	Name      string   `yaml:"name" json:"name"`
	Condition string   `yaml:"condition" json:"condition"`
	Channels  []string `yaml:"channels" json:"channels"`
}

type SlackConfig struct {
	WebhookURL string `yaml:"webhook_url" json:"webhook_url"`
}

type PagerDutyConfig struct {
	RoutingKey string `yaml:"routing_key" json:"routing_key"`
}

type RuleStatus struct {
	Name        string     `json:"name"`
	Condition   string     `json:"condition"`
	Channels    []string   `json:"channels"`
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read alerts config: %w", err)
	}
	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode alerts config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c Config) Validate() error {
	needsSlack := false
	needsPagerDuty := false
	for i, rule := range c.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rules[%d].name is required", i)
		}
		if _, err := ParseCondition(strings.TrimSpace(rule.Condition)); err != nil {
			return fmt.Errorf("rules[%d].condition: %w", i, err)
		}
		if len(rule.Channels) == 0 {
			return fmt.Errorf("rules[%d].channels is required", i)
		}
		for _, channel := range rule.Channels {
			switch strings.ToLower(strings.TrimSpace(channel)) {
			case "slack", "pagerduty":
				if strings.EqualFold(strings.TrimSpace(channel), "slack") {
					needsSlack = true
				}
				if strings.EqualFold(strings.TrimSpace(channel), "pagerduty") {
					needsPagerDuty = true
				}
			default:
				return fmt.Errorf("rules[%d].channels contains unsupported channel %q", i, channel)
			}
		}
	}
	if needsSlack && c.Slack.WebhookURL == "" {
		return fmt.Errorf("slack.webhook_url is required for slack rules")
	}
	if c.Slack.WebhookURL != "" && !strings.HasPrefix(c.Slack.WebhookURL, "http") {
		return fmt.Errorf("slack.webhook_url must be an http(s) url")
	}
	if needsPagerDuty && c.PagerDuty.RoutingKey == "" {
		return fmt.Errorf("pagerduty.routing_key is required for pagerduty rules")
	}
	return nil
}
