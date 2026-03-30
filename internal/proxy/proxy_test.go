package proxy

import (
	"testing"
	"time"

	"mcpscope/internal/intercept"
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
	if _, persist := tracker.Record("demo-server", request); persist {
		t.Fatalf("expected request frame to be held until the response arrives")
	}

	response := intercept.Capture(
		"stdio",
		"server_to_client",
		responseAt,
		responseAt.Add(3*time.Millisecond),
		[]byte(`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`),
	)
	record, persist := tracker.Record("demo-server", response)
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

	record, persist := tracker.Record("demo-server", event)
	if !persist {
		t.Fatalf("expected notification to persist immediately")
	}
	if record.Method != "notifications/tools/list_changed" {
		t.Fatalf("method = %q", record.Method)
	}
	if got := string(record.Params); got != `{"source":"test"}` {
		t.Fatalf("params = %s", got)
	}
}
