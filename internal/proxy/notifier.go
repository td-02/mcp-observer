package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

type deliveryOutcome struct {
	Notification string
	Status       string
	Error        string
	Target       string
	Detail       string
	Attempts     int
}

func deliverAlertNotifications(ctx context.Context, cfg Config, evaluation alertEvaluation) deliveryOutcome {
	targets := buildNotificationTargets(cfg, evaluation)
	if len(targets) == 0 {
		return deliveryOutcome{Status: "skipped"}
	}

	attempts := cfg.NotifyRetries
	if attempts < 1 {
		attempts = 1
	}
	backoff := cfg.NotifyBackoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}

	client := &http.Client{Timeout: 5 * time.Second}
	for _, target := range targets {
		usedAttempts, err := sendWithRetry(ctx, client, target, attempts, backoff)
		if err == nil {
			return deliveryOutcome{
				Notification: target.Kind,
				Status:       "sent",
				Target:       target.Target,
				Detail:       target.Detail,
				Attempts:     usedAttempts,
			}
		}
		if target.Kind == "pagerduty" || target.Kind == "slack" {
			return deliveryOutcome{
				Notification: target.Kind,
				Status:       "failed",
				Error:        err.Error(),
				Target:       target.Target,
				Detail:       target.Detail,
				Attempts:     usedAttempts,
			}
		}
	}

	last := targets[len(targets)-1]
	return deliveryOutcome{
		Notification: last.Kind,
		Status:       "failed",
		Error:        "all notification targets failed",
		Target:       last.Target,
		Detail:       last.Detail,
		Attempts:     attempts,
	}
}

type notificationTarget struct {
	Kind     string
	Target   string
	Detail   string
	Payload  []byte
	Attempts int
}

func buildNotificationTargets(cfg Config, evaluation alertEvaluation) []notificationTarget {
	targets := make([]notificationTarget, 0, len(cfg.NotifyWebhooks)+len(cfg.SlackWebhooks)+len(cfg.PagerDutyKeys))
	for _, webhook := range cfg.NotifyWebhooks {
		payload, _ := json.Marshal(evaluation)
		targets = append(targets, notificationTarget{
			Kind:    "webhook",
			Target:  webhook,
			Detail:  "generic webhook",
			Payload: payload,
		})
	}
	for _, webhook := range cfg.SlackWebhooks {
		payload, _ := json.Marshal(map[string]any{
			"text": fmt.Sprintf("[%s/%s] %s is %s: current %.2f threshold %.2f",
				evaluation.RuleType, evaluation.Status, evaluation.Name, evaluation.Status, evaluation.CurrentValue, evaluation.Threshold),
		})
		targets = append(targets, notificationTarget{
			Kind:    "slack",
			Target:  webhook,
			Detail:  "slack webhook",
			Payload: payload,
		})
	}
	for _, key := range cfg.PagerDutyKeys {
		payload, _ := json.Marshal(map[string]any{
			"routing_key":  key,
			"event_action": "trigger",
			"payload": map[string]any{
				"summary":   fmt.Sprintf("%s %s", evaluation.Name, evaluation.Status),
				"severity":  pagerDutySeverity(evaluation),
				"source":    strings.TrimSpace(evaluation.ServerName),
				"component": "mcpscope",
				"custom_details": map[string]any{
					"rule_id":       evaluation.RuleID,
					"rule_type":     evaluation.RuleType,
					"status":        evaluation.Status,
					"current_value": evaluation.CurrentValue,
					"threshold":     evaluation.Threshold,
				},
			},
		})
		targets = append(targets, notificationTarget{
			Kind:    "pagerduty",
			Target:  pagerDutyEventsURL,
			Detail:  "pagerduty routing key",
			Payload: payload,
		})
	}
	return targets
}

func sendWithRetry(ctx context.Context, client *http.Client, target notificationTarget, maxAttempts int, backoff time.Duration) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.Target, bytes.NewReader(target.Payload))
		if err != nil {
			return attempt, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil && resp != nil {
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return attempt, nil
			}
			err = fmt.Errorf("http %d", resp.StatusCode)
		}
		lastErr = err
		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return attempt, ctx.Err()
			case <-time.After(time.Duration(attempt) * backoff):
			}
		}
	}
	return maxAttempts, lastErr
}

func pagerDutySeverity(evaluation alertEvaluation) string {
	if evaluation.Status == "firing" {
		return "error"
	}
	if evaluation.Status == "ok" {
		return "info"
	}
	return "warning"
}
