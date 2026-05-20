package alerting

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"mcpscope/internal/store"
)

type ConditionKind string

const (
	ConditionErrorRate ConditionKind = "error_rate_5m"
	ConditionP99       ConditionKind = "p99_ms"
)

var conditionPattern = regexp.MustCompile(`^(error_rate_5m|p99_ms)\s*>\s*([0-9]+(?:\.[0-9]+)?)$`)

type Condition struct {
	Kind      ConditionKind
	Threshold float64
	Raw       string
}

func ParseCondition(raw string) (Condition, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Condition{}, fmt.Errorf("condition is required")
	}
	matches := conditionPattern.FindStringSubmatch(raw)
	if len(matches) != 3 {
		return Condition{}, fmt.Errorf("unsupported condition %q", raw)
	}

	threshold, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return Condition{}, fmt.Errorf("parse threshold: %w", err)
	}
	if matches[1] == string(ConditionP99) && threshold < 1 {
		return Condition{}, fmt.Errorf("p99_ms threshold must be >= 1")
	}
	return Condition{
		Kind:      ConditionKind(matches[1]),
		Threshold: threshold,
		Raw:       raw,
	}, nil
}

type ConditionResult struct {
	MetricName string
	Value      float64
}

func EvaluateCondition(condition string, traces []store.Trace) (ConditionResult, bool, error) {
	parsed, err := ParseCondition(condition)
	if err != nil {
		return ConditionResult{}, false, err
	}
	if len(traces) == 0 {
		return ConditionResult{MetricName: string(parsed.Kind)}, false, nil
	}

	switch parsed.Kind {
	case ConditionErrorRate:
		errorsCount := 0
		for _, trace := range traces {
			if trace.IsError {
				errorsCount++
			}
		}
		value := float64(errorsCount) / float64(len(traces))
		return ConditionResult{MetricName: string(parsed.Kind), Value: value}, value > parsed.Threshold, nil
	case ConditionP99:
		values := make([]int64, 0, len(traces))
		for _, trace := range traces {
			values = append(values, trace.LatencyMs)
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		value := float64(percentile(values, 0.99))
		return ConditionResult{MetricName: string(parsed.Kind), Value: value}, value > parsed.Threshold, nil
	default:
		return ConditionResult{}, false, fmt.Errorf("unsupported condition %q", condition)
	}
}
