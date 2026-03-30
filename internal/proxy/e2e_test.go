package proxy

import (
	"context"
	"encoding/json"
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
