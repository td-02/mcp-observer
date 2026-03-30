package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeliverAlertNotificationsSlack(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	outcome := deliverAlertNotifications(context.Background(), Config{
		SlackWebhooks: []string{server.URL},
		NotifyRetries: 2,
		NotifyBackoff: time.Millisecond,
	}, alertEvaluation{
		RuleID:       "rule-1",
		Name:         "Latency",
		RuleType:     "latency_p95",
		Status:       "firing",
		CurrentValue: 500,
		Threshold:    200,
	})

	if outcome.Status != "sent" || outcome.Notification != "slack" {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDeliverAlertNotificationsPagerDutyRetry(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "try again", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	original := pagerDutyEventsURL
	pagerDutyEventsURL = server.URL
	defer func() { pagerDutyEventsURL = original }()

	outcome := deliverAlertNotifications(context.Background(), Config{
		PagerDutyKeys: []string{"routing-key"},
		NotifyRetries: 2,
		NotifyBackoff: time.Millisecond,
	}, alertEvaluation{
		RuleID:       "rule-2",
		Name:         "Errors",
		RuleType:     "error_rate",
		Status:       "firing",
		CurrentValue: 50,
		Threshold:    5,
	})

	if outcome.Status != "sent" || outcome.Notification != "pagerduty" {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	if outcome.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", outcome.Attempts)
	}
}
