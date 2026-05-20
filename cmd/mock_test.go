package cmd

import (
	"testing"

	"mcpscope/internal/replay"
	"mcpscope/internal/store"
)

func TestMockMatchTraceUsesMethodAndParamsHash(t *testing.T) {
	t.Parallel()

	traces := []store.Trace{
		{Method: "tools/call", ParamsPayload: `{"name":"alpha"}`, ParamsHash: replay.ParamsHash([]byte(`{"name":"alpha"}`))},
		{Method: "tools/call", ParamsPayload: `{"name":"beta"}`, ParamsHash: replay.ParamsHash([]byte(`{"name":"beta"}`))},
	}

	trace, ok := mockMatchTrace(traces, "tools/call", []byte(`{"name":"beta"}`))
	if !ok {
		t.Fatalf("expected a trace match")
	}
	if trace.ParamsPayload != `{"name":"beta"}` {
		t.Fatalf("unexpected trace payload: %+v", trace)
	}

	if _, ok := mockMatchTrace(traces, "tools/call", []byte(`{"name":"gamma"}`)); ok {
		t.Fatalf("expected unmatched params to miss")
	}
}

func TestMockMatchParamsHashTracksRawBytes(t *testing.T) {
	t.Parallel()

	left := mockMatchParamsHash("tools/call", []byte(`{"a":1}`))
	right := mockMatchParamsHash("tools/call", []byte(`{"a":1}`))
	if left != right {
		t.Fatalf("expected identical payloads to hash the same")
	}

	if left == mockMatchParamsHash("tools/call", []byte(`{"a":1 }`)) {
		t.Fatalf("expected raw byte differences to change the hash")
	}
}
