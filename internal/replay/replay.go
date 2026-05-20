package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mcpscope/internal/intercept"
	"mcpscope/internal/store"
)

type pathStep struct {
	key   string
	index *int
}

func LoadTraceByID(ctx context.Context, traceStore store.TraceStore, traceID string) (store.Trace, error) {
	if traceStore == nil {
		return store.Trace{}, fmt.Errorf("trace storage unavailable")
	}
	traces, err := traceStore.Query(ctx, store.QueryFilter{TraceID: traceID, Limit: 1})
	if err != nil {
		return store.Trace{}, fmt.Errorf("query trace: %w", err)
	}
	if len(traces) == 0 {
		return store.Trace{}, fmt.Errorf("trace %q not found", traceID)
	}
	return traces[0], nil
}

func LoadAllTraces(ctx context.Context, traceStore store.TraceStore) ([]store.Trace, error) {
	if traceStore == nil {
		return nil, fmt.Errorf("trace storage unavailable")
	}
	traces, err := traceStore.List(ctx, store.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list traces: %w", err)
	}
	return traces, nil
}

func ParamsHash(raw json.RawMessage) string {
	return intercept.HashRaw(raw)
}

func MatchTraceByParams(traces []store.Trace, method string, params json.RawMessage) (store.Trace, bool) {
	expectedHash := ParamsHash(params)
	for _, trace := range traces {
		if trace.Method != method {
			continue
		}
		if trace.ParamsHash != "" && trace.ParamsHash == expectedHash {
			return trace, true
		}
		if trace.ParamsHash == "" && ParamsHash(json.RawMessage(trace.ParamsPayload)) == expectedHash {
			return trace, true
		}
	}
	return store.Trace{}, false
}

func BuildJSONRPCRequest(trace store.Trace, requestID any) ([]byte, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  trace.Method,
		"params":  json.RawMessage(trace.ParamsPayload),
	}
	return json.Marshal(payload)
}

func InvokeHTTP(ctx context.Context, target string, trace store.Trace) ([]byte, int64, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, 0, fmt.Errorf("missing replay target URL")
	}

	if _, err := url.ParseRequestURI(target); err != nil {
		return nil, 0, fmt.Errorf("invalid replay target URL: %w", err)
	}

	client := &http.Client{}
	payload, err := BuildJSONRPCRequest(trace, trace.TraceID)
	if err != nil {
		return nil, 0, fmt.Errorf("build replay request: %w", err)
	}

	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("create replay request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send replay request: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read replay response: %w", err)
	}

	return body, time.Since(started).Milliseconds(), nil
}

func CompareResponses(original, actual []byte, ignoreFields []string) (bool, string, error) {
	left, err := canonicalJSON(original, ignoreFields)
	if err != nil {
		return false, "", err
	}
	right, err := canonicalJSON(unwrappedComparablePayload(actual), ignoreFields)
	if err != nil {
		return false, "", err
	}

	if bytes.Equal(left, right) {
		return true, "", nil
	}

	diff := formatSideBySide(splitLines(string(left)), splitLines(string(right)))
	return false, diff, nil
}

func unwrappedComparablePayload(raw []byte) []byte {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return raw
	}

	if len(envelope["jsonrpc"]) == 0 {
		return raw
	}
	if result, ok := envelope["result"]; ok && len(result) > 0 {
		return result
	}
	if errPayload, ok := envelope["error"]; ok && len(errPayload) > 0 {
		return errPayload
	}
	return raw
}

func canonicalJSON(raw []byte, ignoreFields []string) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte("null"), nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return bytes.TrimSpace(raw), nil
	}

	for _, field := range ignoreFields {
		removeJSONPath(&value, parseJSONPath(field))
	}

	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal canonical json: %w", err)
	}
	return encoded, nil
}

func parseJSONPath(raw string) []pathStep {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "$")
	raw = strings.TrimPrefix(raw, ".")
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ".")
	steps := make([]pathStep, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		steps = append(steps, parsePathPart(part))
	}
	return steps
}

func parsePathPart(part string) pathStep {
	step := pathStep{}
	if idx := strings.Index(part, "["); idx >= 0 {
		step.key = part[:idx]
		if end := strings.Index(part[idx:], "]"); end > 1 {
			if parsed, err := strconv.Atoi(part[idx+1 : idx+end]); err == nil {
				step.index = &parsed
			}
		}
		return step
	}
	step.key = part
	return step
}

func removeJSONPath(value *any, steps []pathStep) {
	if len(steps) == 0 || value == nil {
		return
	}

	current := *value
	step := steps[0]

	switch node := current.(type) {
	case map[string]any:
		child, ok := node[step.key]
		if !ok {
			return
		}
		if len(steps) == 1 && step.index == nil {
			delete(node, step.key)
			*value = node
			return
		}
		if step.index == nil {
			removeJSONPath(&child, steps[1:])
			node[step.key] = child
			*value = node
			return
		}
		arr, ok := child.([]any)
		if !ok || *step.index < 0 || *step.index >= len(arr) {
			return
		}
		if len(steps) == 1 {
			node[step.key] = append(arr[:*step.index], arr[*step.index+1:]...)
			*value = node
			return
		}
		elem := arr[*step.index]
		removeJSONPath(&elem, steps[1:])
		arr[*step.index] = elem
		node[step.key] = arr
		*value = node
	case []any:
		if step.key != "" {
			return
		}
		if step.index == nil || *step.index < 0 || *step.index >= len(node) {
			return
		}
		if len(steps) == 1 {
			*value = append(node[:*step.index], node[*step.index+1:]...)
			return
		}
		elem := node[*step.index]
		removeJSONPath(&elem, steps[1:])
		node[*step.index] = elem
		*value = node
	}
}

func splitLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{""}
	}
	lines := strings.Split(raw, "\n")
	return lines
}

func formatSideBySide(left, right []string) string {
	width := 0
	for _, line := range left {
		if len(line) > width {
			width = len(line)
		}
	}
	if width < 40 {
		width = 40
	}

	var buf strings.Builder
	maxLines := len(left)
	if len(right) > maxLines {
		maxLines = len(right)
	}
	for i := 0; i < maxLines; i++ {
		l := ""
		r := ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		marker := " "
		if l != r {
			marker = "!"
		}
		fmt.Fprintf(&buf, "%s %-*s | %s\n", marker, width, l, r)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func ioReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
