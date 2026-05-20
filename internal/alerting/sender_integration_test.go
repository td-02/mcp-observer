//go:build integration

package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlackSenderIntegration(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("content type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := SlackSender{
		WebhookURL: server.URL,
		PublicURL:  "https://mcpscope.example.com",
	}.Send(context.Background(), RuleConfig{
		Name:      "high-error-rate",
		Condition: "error_rate_5m > 0.10",
		Channels:  []string{"slack"},
	}, ConditionResult{
		MetricName: string(ConditionErrorRate),
		Value:      0.1234,
	}, time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	text, _ := got["text"].(string)
	if !strings.Contains(text, "high-error-rate") || !strings.Contains(text, "Timestamp:") || !strings.Contains(text, "Dashboard:") {
		t.Fatalf("unexpected slack payload: %#v", got)
	}
}
