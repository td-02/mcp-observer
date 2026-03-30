package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"

	"mcpscope/internal/store"
)

func TestFilterReplayTraces(t *testing.T) {
	t.Parallel()

	traces := []store.Trace{
		{TraceID: "1", Workspace: "acme", Environment: "prod"},
		{TraceID: "2", Workspace: "beta", Environment: "prod"},
		{TraceID: "3", Workspace: "acme", Environment: "stage"},
	}

	filtered := filterReplayTraces(traces, "acme", "prod")
	if len(filtered) != 1 || filtered[0].TraceID != "1" {
		t.Fatalf("unexpected filtered traces: %+v", filtered)
	}
}

func TestFinalizeReplayEnforcesThresholds(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	report := replayReport{
		TotalRequests: 2,
		SuccessCount:  1,
		ErrorCount:    1,
		MaxLatencyMs:  250,
	}

	if err := finalizeReplay(cmd, report, "", false, 300); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Len() == 0 {
		t.Fatalf("expected replay summary output")
	}
	if err := finalizeReplay(cmd, report, "", true, 0); err == nil {
		t.Fatalf("expected fail-on-error to trigger")
	}
	if err := finalizeReplay(cmd, report, "", false, 200); err == nil {
		t.Fatalf("expected latency threshold to trigger")
	}
}
