package auditexport

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"mcpscope/internal/store"
)

const (
	FormatCSV  = "csv"
	FormatJSON = "json"
)

type Record struct {
	TraceID      string `json:"trace_id"`
	Timestamp    string `json:"timestamp"`
	Method       string `json:"method"`
	ToolName     string `json:"tool_name"`
	DurationMs   int64  `json:"duration_ms"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
	ParamsJSON   string `json:"params_json"`
	ResponseJSON string `json:"response_json"`
}

type FilterInput struct {
	Workspace   string
	Environment string
	Method      string
	Status      string
	From        string
	To          string
}

func RecordFromTrace(trace store.Trace) Record {
	status := "ok"
	if trace.IsError {
		status = "error"
	}

	return Record{
		TraceID:      trace.TraceID,
		Timestamp:    trace.CreatedAt.UTC().Format(time.RFC3339Nano),
		Method:       trace.Method,
		ToolName:     ToolNameFromParams(trace.ParamsPayload),
		DurationMs:   trace.LatencyMs,
		Status:       status,
		ErrorMessage: trace.ErrorMessage,
		ParamsJSON:   trace.ParamsPayload,
		ResponseJSON: trace.ResponsePayload,
	}
}

func BuildQueryFilter(input FilterInput, now time.Time) (store.QueryFilter, error) {
	filter := store.QueryFilter{
		Workspace:   strings.TrimSpace(input.Workspace),
		Environment: strings.TrimSpace(input.Environment),
		Method:      strings.TrimSpace(input.Method),
	}

	status, err := ParseStatusFilter(input.Status)
	if err != nil {
		return store.QueryFilter{}, err
	}
	switch status {
	case "ok":
		value := false
		filter.IsError = &value
	case "error":
		value := true
		filter.IsError = &value
	}

	if strings.TrimSpace(input.From) != "" {
		parsed, err := ParseTimeBound(input.From, now, true)
		if err != nil {
			return store.QueryFilter{}, fmt.Errorf("from: %w", err)
		}
		filter.CreatedAfter = &parsed
	}
	if strings.TrimSpace(input.To) != "" {
		parsed, err := ParseTimeBound(input.To, now, false)
		if err != nil {
			return store.QueryFilter{}, fmt.Errorf("to: %w", err)
		}
		filter.CreatedBefore = &parsed
	}

	return filter, nil
}

func ToolNameFromParams(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}

	obj, ok := value.(map[string]any)
	if !ok {
		return ""
	}

	for _, key := range []string{"tool_name", "name"} {
		if text, ok := obj[key].(string); ok {
			return strings.TrimSpace(text)
		}
	}

	return ""
}

func ParseStatusFilter(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "all", nil
	}
	switch raw {
	case "all", "ok", "error":
		return raw, nil
	default:
		return "", fmt.Errorf("invalid status %q", raw)
	}
}

func ParseTimeBound(raw string, now time.Time, isStart bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if strings.EqualFold(raw, "now") {
		return now.UTC(), nil
	}

	if duration, ok := parseRelativeDuration(raw); ok {
		return now.Add(-duration).UTC(), nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}

	if isStart {
		return time.Time{}, fmt.Errorf("invalid from time %q", raw)
	}
	return time.Time{}, fmt.Errorf("invalid to time %q", raw)
}

func WriteCSVHeader(w *csv.Writer) error {
	return w.Write([]string{
		"trace_id",
		"timestamp",
		"method",
		"tool_name",
		"duration_ms",
		"status",
		"error_message",
		"params_json",
		"response_json",
	})
}

func WriteCSVRecord(w *csv.Writer, record Record) error {
	return w.Write([]string{
		record.TraceID,
		record.Timestamp,
		record.Method,
		record.ToolName,
		fmt.Sprintf("%d", record.DurationMs),
		record.Status,
		record.ErrorMessage,
		record.ParamsJSON,
		record.ResponseJSON,
	})
}

func WriteJSONRecord(w io.Writer, record Record) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(encoded))
	return err
}

func StreamRows(rows *sql.Rows, format string, w io.Writer) error {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "", FormatJSON:
		return streamJSON(rows, w)
	case FormatCSV:
		return streamCSV(rows, w)
	default:
		return fmt.Errorf("unsupported export format %q", format)
	}
}

func streamCSV(rows *sql.Rows, w io.Writer) error {
	writer := csv.NewWriter(w)
	if err := WriteCSVHeader(writer); err != nil {
		return err
	}

	for rows.Next() {
		trace, err := scanTrace(rows)
		if err != nil {
			return err
		}
		if err := WriteCSVRecord(writer, RecordFromTrace(trace)); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return rows.Err()
}

func streamJSON(rows *sql.Rows, w io.Writer) error {
	encoder := json.NewEncoder(w)
	for rows.Next() {
		trace, err := scanTrace(rows)
		if err != nil {
			return err
		}
		if err := encoder.Encode(RecordFromTrace(trace)); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanTrace(rows *sql.Rows) (store.Trace, error) {
	var trace store.Trace
	var sdkReported int
	var createdAtRaw string
	if err := rows.Scan(
		&trace.ID,
		&trace.TraceID,
		&trace.Workspace,
		&trace.Environment,
		&trace.ServerID,
		&trace.ServerName,
		&trace.Method,
		&trace.ParamsHash,
		&trace.ParamsPayload,
		&trace.ResponseHash,
		&trace.ResponsePayload,
		&trace.LatencyMs,
		&trace.IsError,
		&trace.ErrorMessage,
		&sdkReported,
		&createdAtRaw,
	); err != nil {
		return store.Trace{}, fmt.Errorf("scan trace: %w", err)
	}

	trace.SdkReported = sdkReported != 0
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return store.Trace{}, fmt.Errorf("parse trace timestamp: %w", err)
	}
	trace.CreatedAt = createdAt.UTC()
	return trace, nil
}

func parseRelativeDuration(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	units := map[byte]time.Duration{
		's': time.Second,
		'm': time.Minute,
		'h': time.Hour,
		'd': 24 * time.Hour,
		'w': 7 * 24 * time.Hour,
	}
	last := raw[len(raw)-1]
	unit, ok := units[last]
	if !ok {
		return 0, false
	}

	valuePart := strings.TrimSpace(raw[:len(raw)-1])
	if valuePart == "" {
		return 0, false
	}

	value, err := time.ParseDuration(valuePart + string(last))
	if err == nil {
		return value, true
	}

	parsed, err := strconv.Atoi(valuePart)
	if err != nil {
		return 0, false
	}
	return time.Duration(parsed) * unit, true
}
