package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

type Sender interface {
	Send(ctx context.Context, rule RuleConfig, result ConditionResult, firedAt time.Time) error
}

type SlackSender struct {
	WebhookURL string
	PublicURL  string
	Client     *http.Client
}

func (s SlackSender) Send(ctx context.Context, rule RuleConfig, result ConditionResult, firedAt time.Time) error {
	if strings.TrimSpace(s.WebhookURL) == "" {
		return fmt.Errorf("missing slack webhook url")
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	text := fmt.Sprintf("[mcpscope] Rule '%s' fired — %s: %s", rule.Name, result.MetricName, formatMetricValue(result))
	payload := map[string]any{
		"text": text + "\n" + fmt.Sprintf("Timestamp: %s", firedAt.UTC().Format(time.RFC3339)),
	}
	if dash := buildDashboardLink(s.PublicURL); dash != "" {
		payload["text"] = payload["text"].(string) + "\nDashboard: " + dash
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %s", resp.Status)
	}
	return nil
}

type PagerDutySender struct {
	RoutingKey string
	Client     *http.Client
	PublicURL  string
}

func (s PagerDutySender) Send(ctx context.Context, rule RuleConfig, result ConditionResult, firedAt time.Time) error {
	if strings.TrimSpace(s.RoutingKey) == "" {
		return fmt.Errorf("missing pagerduty routing key")
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	severity := "warning"
	if result.MetricName == string(ConditionErrorRate) {
		severity = "error"
	}

	payload := map[string]any{
		"routing_key":  s.RoutingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":   fmt.Sprintf("[mcpscope] Rule '%s' fired", rule.Name),
			"severity":  severity,
			"source":    "mcpscope",
			"timestamp": firedAt.UTC().Format(time.RFC3339),
			"custom_details": map[string]any{
				"rule_name":     rule.Name,
				"condition":     rule.Condition,
				"metric":        result.MetricName,
				"value":         result.Value,
				"dashboard_url": buildDashboardLink(s.PublicURL),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pagerDutyEventsURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned %s", resp.Status)
	}
	return nil
}

func buildDashboardLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return parsed.String()
}

func formatMetricValue(result ConditionResult) string {
	if result.MetricName == string(ConditionErrorRate) {
		return fmt.Sprintf("%.2f%%", result.Value*100)
	}
	return fmt.Sprintf("%.0fms", result.Value)
}
