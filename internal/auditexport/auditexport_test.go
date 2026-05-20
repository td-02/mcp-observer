package auditexport

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"mcpscope/internal/store"
)

func TestCSVSerialization(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := WriteCSVHeader(writer); err != nil {
		t.Fatalf("WriteCSVHeader returned error: %v", err)
	}
	record := Record{
		TraceID:      "trace-1",
		Timestamp:    "2026-03-31T10:00:00Z",
		Method:       "tools/call",
		ToolName:     "search",
		DurationMs:   42,
		Status:       "ok",
		ErrorMessage: "",
		ParamsJSON:   `{"name":"search"}`,
		ResponseJSON: `{"ok":true}`,
	}
	if err := WriteCSVRecord(writer, record); err != nil {
		t.Fatalf("WriteCSVRecord returned error: %v", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("writer.Error returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected csv output: %q", buf.String())
	}
	if !strings.Contains(lines[1], "trace-1") || !strings.Contains(lines[1], "search") {
		t.Fatalf("unexpected csv row: %q", lines[1])
	}
}

func TestJSONSerialization(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	record := Record{
		TraceID:      "trace-1",
		Timestamp:    "2026-03-31T10:00:00Z",
		Method:       "tools/call",
		ToolName:     "search",
		DurationMs:   42,
		Status:       "error",
		ErrorMessage: "boom",
		ParamsJSON:   `{"name":"search"}`,
		ResponseJSON: `{"error":"boom"}`,
	}
	if err := WriteJSONRecord(&buf, record); err != nil {
		t.Fatalf("WriteJSONRecord returned error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); !strings.Contains(got, `"trace_id":"trace-1"`) || !strings.Contains(got, `"status":"error"`) {
		t.Fatalf("unexpected json output: %s", got)
	}
}

func TestRecordFromTraceExtractsToolName(t *testing.T) {
	t.Parallel()

	trace := store.Trace{
		TraceID:         "trace-1",
		Method:          "tools/call",
		ParamsPayload:   `{"name":"search_web"}`,
		ResponsePayload: `{"ok":true}`,
		LatencyMs:       17,
		CreatedAt:       time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC),
	}

	record := RecordFromTrace(trace)
	if record.ToolName != "search_web" {
		t.Fatalf("tool_name = %q", record.ToolName)
	}
	if record.Status != "ok" {
		t.Fatalf("status = %q", record.Status)
	}
}
