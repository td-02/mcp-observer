package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcpscope/internal/intercept"
	"mcpscope/internal/store"
)

func TestTraceTrackerCorrelatesRequestAndResponse(t *testing.T) {
	t.Parallel()

	tracker := newTraceTracker()
	requestAt := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	responseAt := requestAt.Add(75 * time.Millisecond)

	request := intercept.Capture(
		"stdio",
		"client_to_server",
		requestAt,
		requestAt.Add(2*time.Millisecond),
		[]byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"alpha"}}`),
	)
	if _, persist := tracker.Record("demo-server-id", "demo-server", "", request); persist {
		t.Fatalf("expected request frame to be held until the response arrives")
	}

	response := intercept.Capture(
		"stdio",
		"server_to_client",
		responseAt,
		responseAt.Add(3*time.Millisecond),
		[]byte(`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`),
	)
	record, persist := tracker.Record("demo-server-id", "demo-server", "", response)
	if !persist {
		t.Fatalf("expected correlated response to produce a trace")
	}

	if record.TraceID != request.TraceID {
		t.Fatalf("trace_id = %q, want %q", record.TraceID, request.TraceID)
	}
	if record.Method != "tools/call" {
		t.Fatalf("method = %q", record.Method)
	}
	if got := string(record.Params); got != `{"name":"alpha"}` {
		t.Fatalf("params = %s", got)
	}
	if got := string(record.Response); got != `{"ok":true}` {
		t.Fatalf("response = %s", got)
	}
	if record.LatencyMs != 78 {
		t.Fatalf("latency_ms = %d, want 78", record.LatencyMs)
	}
	if record.ServerID != "demo-server-id" {
		t.Fatalf("server_id = %q, want %q", record.ServerID, "demo-server-id")
	}
}

func TestHTTPMiddlewareSetsSecurityHeaders(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	prepareConfig(&cfg)
	handler := newHTTPHandler(cfg, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
}

func TestAPIRateLimit(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	prepareConfig(&cfg)
	handler := newHTTPHandler(cfg, nil)

	limited := false
	for i := 0; i < 130; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
		req.RemoteAddr = "198.51.100.12:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatalf("expected API limiter to reject excessive requests")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	prepareConfig(&cfg)
	_ = persistTraceRecord(context.Background(), cfg, traceAPIRecord{
		ID:        "trace-1",
		TraceID:   "trace-1",
		ServerID:  "server-a",
		Method:    "tools/call",
		Status:    "success",
		LatencyMs: 120,
		CreatedAt: time.Now().UTC(),
	})
	handler := newHTTPHandler(cfg, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	text := string(body)

	if !strings.Contains(text, "mcpscope_traces_total") {
		t.Fatalf("metrics output missing traces counter: %s", text)
	}
	if !strings.Contains(text, "mcpscope_proxy_duration_seconds_bucket") {
		t.Fatalf("metrics output missing duration histogram: %s", text)
	}
	if !strings.Contains(text, "mcpscope_active_connections") {
		t.Fatalf("metrics output missing active connections gauge: %s", text)
	}
}

func TestTraceTrackerPersistsNotificationsImmediately(t *testing.T) {
	t.Parallel()

	tracker := newTraceTracker()
	event := intercept.Capture(
		"http",
		"client_to_server",
		time.Date(2026, 3, 30, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 30, 11, 0, 0, int(5*time.Millisecond), time.UTC),
		[]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{"source":"test"}}`),
	)

	record, persist := tracker.Record("demo-server-id", "demo-server", "", event)
	if !persist {
		t.Fatalf("expected notification to persist immediately")
	}
	if record.Method != "notifications/tools/list_changed" {
		t.Fatalf("method = %q", record.Method)
	}
	if got := string(record.Params); got != `{"source":"test"}` {
		t.Fatalf("params = %s", got)
	}
	if record.ServerID != "demo-server-id" {
		t.Fatalf("server_id = %q, want %q", record.ServerID, "demo-server-id")
	}
}

func TestCaptureAndPersistStoresServerID(t *testing.T) {
	t.Parallel()

	var saved store.Trace
	cfg := Config{
		ServerID:   "server-a",
		ServerName: "server-a",
		Store: capturingTraceStore{
			insert: func(trace store.Trace) error {
				saved = trace
				return nil
			},
		},
	}
	prepareConfig(&cfg)

	if err := captureAndPersist(
		context.Background(),
		cfg,
		"",
		"http",
		"client_to_server",
		time.Date(2026, 3, 31, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 31, 11, 0, 0, int(5*time.Millisecond), time.UTC),
		[]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{"source":"test"}}`),
	); err != nil {
		t.Fatalf("captureAndPersist returned error: %v", err)
	}

	if saved.ServerID != "server-a" {
		t.Fatalf("server_id = %q, want %q", saved.ServerID, "server-a")
	}
}

func TestRequireAuthAllowsBearerAndQueryToken(t *testing.T) {
	t.Parallel()

	cfg := Config{AuthToken: "secret-token"}
	handler := requireAuth(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name   string
		target string
		header string
		want   int
	}{
		{name: "missing", target: "/api/traces", want: http.StatusUnauthorized},
		{name: "bearer", target: "/api/traces", header: "Bearer secret-token", want: http.StatusNoContent},
		{name: "query", target: "/events?token=secret-token", want: http.StatusNoContent},
		{name: "dashboard asset", target: "/", want: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.target, nil)
			if test.header != "" {
				req.Header.Set("Authorization", test.header)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}

type capturingTraceStore struct {
	insert func(store.Trace) error
}

func (c capturingTraceStore) Insert(ctx context.Context, trace store.Trace) error {
	if c.insert != nil {
		return c.insert(trace)
	}
	return nil
}

func (c capturingTraceStore) Query(context.Context, store.QueryFilter) ([]store.Trace, error) {
	return nil, nil
}

func (c capturingTraceStore) List(context.Context, store.ListOptions) ([]store.Trace, error) {
	return nil, nil
}

func (c capturingTraceStore) DeleteOlderThan(context.Context, time.Time) error {
	return nil
}

func (c capturingTraceStore) TrimToCount(context.Context, int) error {
	return nil
}

func (c capturingTraceStore) UpsertAlertRule(context.Context, store.AlertRule) (store.AlertRule, error) {
	return store.AlertRule{}, nil
}

func (c capturingTraceStore) ListAlertRules(context.Context) ([]store.AlertRule, error) {
	return nil, nil
}

func (c capturingTraceStore) DeleteAlertRule(context.Context, string) error {
	return nil
}

func (c capturingTraceStore) InsertAlertEvent(context.Context, store.AlertEvent) error {
	return nil
}

func (c capturingTraceStore) ListAlertEvents(context.Context, string, string, int) ([]store.AlertEvent, error) {
	return nil, nil
}

func (c capturingTraceStore) LatestAlertEvent(context.Context, string, string, string) (*store.AlertEvent, error) {
	return nil, nil
}

func (c capturingTraceStore) QueryLatencyStats(context.Context, store.QueryFilter) ([]store.LatencyStat, error) {
	return nil, nil
}

func (c capturingTraceStore) QueryErrorStats(context.Context, store.QueryFilter) ([]store.ErrorStat, error) {
	return nil, nil
}

func (c capturingTraceStore) GetBudgetUsage(context.Context, string, string, time.Time) (store.BudgetUsage, error) {
	return store.BudgetUsage{}, nil
}

func (c capturingTraceStore) ListBudgetUsage(context.Context) ([]store.BudgetUsage, error) {
	return nil, nil
}

func (c capturingTraceStore) IncrementBudgetUsage(context.Context, store.BudgetUsage) error {
	return nil
}

func (c capturingTraceStore) ResetBudgetWindow(context.Context, string, string, time.Time) error {
	return nil
}
