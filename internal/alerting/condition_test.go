package alerting

import (
	"testing"

	"mcpscope/internal/store"
)

func TestEvaluateConditionErrorRate(t *testing.T) {
	t.Parallel()

	traces := []store.Trace{
		{IsError: false},
		{IsError: true},
		{IsError: true},
		{IsError: false},
	}

	result, fired, err := EvaluateCondition("error_rate_5m > 0.40", traces)
	if err != nil {
		t.Fatalf("EvaluateCondition returned error: %v", err)
	}
	if !fired {
		t.Fatalf("expected condition to fire")
	}
	if result.MetricName != string(ConditionErrorRate) {
		t.Fatalf("metric = %q", result.MetricName)
	}
	if result.Value != 0.5 {
		t.Fatalf("value = %v, want 0.5", result.Value)
	}
}

func TestEvaluateConditionP99(t *testing.T) {
	t.Parallel()

	traces := []store.Trace{
		{LatencyMs: 10},
		{LatencyMs: 20},
		{LatencyMs: 30},
		{LatencyMs: 40},
		{LatencyMs: 5000},
	}

	result, fired, err := EvaluateCondition("p99_ms > 1000", traces)
	if err != nil {
		t.Fatalf("EvaluateCondition returned error: %v", err)
	}
	if !fired {
		t.Fatalf("expected condition to fire")
	}
	if result.MetricName != string(ConditionP99) {
		t.Fatalf("metric = %q", result.MetricName)
	}
	if result.Value != 5000 {
		t.Fatalf("value = %v, want 5000", result.Value)
	}
}
