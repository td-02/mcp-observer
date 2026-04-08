package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcpscope/internal/store"
)

func TestHTTPHandlerWorkspaceScopedAPIs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mcpscope.db")
	traceStore, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer traceStore.Close()

	createdAt := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	for _, trace := range []store.Trace{
		{ID: "1", TraceID: "t1", Workspace: "acme", Environment: "prod", ServerName: "srv", Method: "tools/call", ParamsHash: "a", ParamsPayload: `{}`, ResponseHash: "a", ResponsePayload: `{}`, CreatedAt: createdAt},
		{ID: "2", TraceID: "t2", Workspace: "beta", Environment: "prod", ServerName: "srv", Method: "tools/call", ParamsHash: "b", ParamsPayload: `{}`, ResponseHash: "b", ResponsePayload: `{}`, CreatedAt: createdAt.Add(time.Second)},
	} {
		if err := traceStore.Insert(ctx, trace); err != nil {
			t.Fatalf("Insert returned error: %v", err)
		}
	}

	handler := newHTTPHandler(Config{
		Workspace:   "acme",
		Environment: "prod",
		AuthToken:   "secret",
		Store:       traceStore,
		Dashboard:   os.DirFS("."),
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/traces?workspace=acme&environment=prod", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload traceListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Workspace != "acme" {
		t.Fatalf("unexpected traces payload: %+v", payload)
	}

	exportReq := httptest.NewRequest(http.MethodGet, "/api/export/traces?workspace=acme&environment=prod", nil)
	exportReq.Header.Set("Authorization", "Bearer secret")
	exportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(exportRecorder, exportReq)
	if exportRecorder.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", exportRecorder.Code, exportRecorder.Body.String())
	}
	if strings.Contains(exportRecorder.Body.String(), `"workspace":"beta"`) {
		t.Fatalf("export leaked another workspace: %s", exportRecorder.Body.String())
	}
}

func TestHTTPHandlerAlertRulesRespectWorkspace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mcpscope.db")
	traceStore, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer traceStore.Close()

	handler := newHTTPHandler(Config{
		Workspace:   "acme",
		Environment: "prod",
		AuthToken:   "secret",
		Store:       traceStore,
		Dashboard:   os.DirFS("."),
	}, nil)

	body := `{"name":"High latency","rule_type":"latency_p95","threshold":200,"window_minutes":15,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/alerts/rules?workspace=acme&environment=prod", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	var saved store.AlertRule
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if saved.Workspace != "acme" || saved.Environment != "prod" {
		t.Fatalf("unexpected saved rule: %+v", saved)
	}
}

func TestHTTPHandlerHealthAndReadiness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mcpscope.db")
	traceStore, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer traceStore.Close()

	handler := newHTTPHandler(Config{
		Workspace:   "acme",
		Environment: "prod",
		Store:       traceStore,
		Dashboard:   os.DirFS("."),
	}, nil)

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", healthRecorder.Code, healthRecorder.Body.String())
	}
	if !strings.Contains(healthRecorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("health response missing ok status: %s", healthRecorder.Body.String())
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readyRecorder, readyReq)
	if readyRecorder.Code != http.StatusOK {
		t.Fatalf("ready status = %d body=%s", readyRecorder.Code, readyRecorder.Body.String())
	}
	if !strings.Contains(readyRecorder.Body.String(), `"ready":true`) {
		t.Fatalf("ready response missing ready=true: %s", readyRecorder.Body.String())
	}
}

func TestCaptureAndPersistBestEffortDoesNotAbortOnStoreFailure(t *testing.T) {
	t.Parallel()

	var stderr strings.Builder
	cfg := Config{
		Store:     failingTraceStore{},
		Stderr:    &stderr,
		Dashboard: os.DirFS("."),
	}

	captureAndPersistBestEffort(
		context.Background(),
		cfg,
		"http",
		"client_to_server",
		time.Date(2026, 3, 31, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 31, 11, 0, 0, int(5*time.Millisecond), time.UTC),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"message":"ping"}}`),
	)

	if !strings.Contains(stderr.String(), "trace capture failed") {
		t.Fatalf("expected best-effort warning, got %q", stderr.String())
	}
}

type failingTraceStore struct{}

func (f failingTraceStore) Insert(context.Context, store.Trace) error {
	return errors.New("insert failed")
}

func (f failingTraceStore) Query(context.Context, store.QueryFilter) ([]store.Trace, error) {
	return nil, nil
}

func (f failingTraceStore) List(context.Context, store.ListOptions) ([]store.Trace, error) {
	return nil, nil
}

func (f failingTraceStore) DeleteOlderThan(context.Context, time.Time) error {
	return nil
}

func (f failingTraceStore) TrimToCount(context.Context, int) error {
	return nil
}

func (f failingTraceStore) UpsertAlertRule(context.Context, store.AlertRule) (store.AlertRule, error) {
	return store.AlertRule{}, nil
}

func (f failingTraceStore) ListAlertRules(context.Context) ([]store.AlertRule, error) {
	return nil, nil
}

func (f failingTraceStore) DeleteAlertRule(context.Context, string) error {
	return nil
}

func (f failingTraceStore) InsertAlertEvent(context.Context, store.AlertEvent) error {
	return nil
}

func (f failingTraceStore) ListAlertEvents(context.Context, string, string, int) ([]store.AlertEvent, error) {
	return nil, nil
}

func (f failingTraceStore) LatestAlertEvent(context.Context, string, string, string) (*store.AlertEvent, error) {
	return nil, nil
}

func (f failingTraceStore) QueryLatencyStats(context.Context, store.QueryFilter) ([]store.LatencyStat, error) {
	return nil, nil
}

func (f failingTraceStore) QueryErrorStats(context.Context, store.QueryFilter) ([]store.ErrorStat, error) {
	return nil, nil
}
