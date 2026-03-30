package appconfig

import (
	"path/filepath"
	"testing"
	"time"

	"os"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mcpscope.json")
	err := os.WriteFile(path, []byte(`{
  "version": 1,
  "workspace": "acme",
  "environment": "prod",
  "authToken": "top-secret",
  "notification": {
    "webhookUrls": ["https://example.invalid/a"],
    "slackWebhookUrls": ["https://hooks.slack.com/services/test"],
    "pagerDutyRoutingKeys": ["routing-key"],
    "retryMaxAttempts": 4,
    "retryBackoffSeconds": 3
  },
  "proxy": {
    "db": "traces.db",
    "port": 5555,
    "transport": "http",
    "retainFor": "72h",
    "maxTraces": 123,
    "redactKeys": ["token"],
    "otel": true
  }
}`), 0o644)
	if err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Environment != "prod" || cfg.AuthToken != "top-secret" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Workspace != "acme" || cfg.Version != 1 {
		t.Fatalf("unexpected workspace/version: %+v", cfg)
	}
	if cfg.Proxy.Port != 5555 || cfg.Proxy.Transport != "http" || !cfg.Proxy.EnableOTEL {
		t.Fatalf("unexpected proxy config: %+v", cfg.Proxy)
	}
	if cfg.Notification.RetryMaxAttempts != 4 || cfg.Notification.RetryBackoffSeconds != 3 {
		t.Fatalf("unexpected notification config: %+v", cfg.Notification)
	}
	if got := cfg.RetentionDuration(); got != 72*time.Hour {
		t.Fatalf("RetentionDuration = %s", got)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mcpscope.json")
	if err := os.WriteFile(path, []byte(`{"version": 2}`), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected config version validation error")
	}
}
