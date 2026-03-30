package store

import (
	"context"
	"time"
)

type Trace struct {
	ID              string    `json:"id"`
	TraceID         string    `json:"trace_id"`
	Workspace       string    `json:"workspace"`
	Environment     string    `json:"environment"`
	ServerName      string    `json:"server_name"`
	Method          string    `json:"method"`
	ParamsHash      string    `json:"params_hash"`
	ParamsPayload   string    `json:"params_payload"`
	ResponseHash    string    `json:"response_hash"`
	ResponsePayload string    `json:"response_payload"`
	LatencyMs       int64     `json:"latency_ms"`
	IsError         bool      `json:"is_error"`
	ErrorMessage    string    `json:"error_message"`
	CreatedAt       time.Time `json:"created_at"`
}

type QueryFilter struct {
	TraceID      string
	Workspace    string
	Environment  string
	ServerName   string
	Method       string
	IsError      *bool
	CreatedAfter *time.Time
	Offset       int
	Limit        int
}

type ListOptions struct {
	Limit  int
	Offset int
}

type AlertRule struct {
	ID            string    `json:"id"`
	Workspace     string    `json:"workspace"`
	Environment   string    `json:"environment"`
	Name          string    `json:"name"`
	RuleType      string    `json:"rule_type"`
	Threshold     float64   `json:"threshold"`
	WindowMinutes int       `json:"window_minutes"`
	ServerName    string    `json:"server_name,omitempty"`
	Method        string    `json:"method,omitempty"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type AlertEvent struct {
	ID               string    `json:"id"`
	RuleID           string    `json:"rule_id"`
	Workspace        string    `json:"workspace"`
	Environment      string    `json:"environment"`
	RuleName         string    `json:"rule_name"`
	Status           string    `json:"status"`
	PreviousStatus   string    `json:"previous_status"`
	CurrentValue     float64   `json:"current_value"`
	Threshold        float64   `json:"threshold"`
	SampleCount      int       `json:"sample_count"`
	Notification     string    `json:"notification"`
	DeliveryStatus   string    `json:"delivery_status"`
	DeliveryError    string    `json:"delivery_error"`
	DeliveryTarget   string    `json:"delivery_target"`
	DeliveryDetail   string    `json:"delivery_detail"`
	DeliveryAttempts int       `json:"delivery_attempts"`
	CreatedAt        time.Time `json:"created_at"`
}

type LatencyStat struct {
	Workspace   string
	Environment string
	ServerName  string
	Method      string
	Count       int
	P50Ms       int64
	P95Ms       int64
	P99Ms       int64
}

type ErrorStat struct {
	Workspace          string
	Environment        string
	Method             string
	Count              int
	ErrorCount         int
	ErrorRatePct       float64
	RecentErrorMessage string
	RecentErrorAt      *time.Time
}

type TraceStore interface {
	Insert(ctx context.Context, trace Trace) error
	Query(ctx context.Context, filter QueryFilter) ([]Trace, error)
	List(ctx context.Context, opts ListOptions) ([]Trace, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) error
	TrimToCount(ctx context.Context, keep int) error
	UpsertAlertRule(ctx context.Context, rule AlertRule) (AlertRule, error)
	ListAlertRules(ctx context.Context) ([]AlertRule, error)
	DeleteAlertRule(ctx context.Context, id string) error
	InsertAlertEvent(ctx context.Context, event AlertEvent) error
	ListAlertEvents(ctx context.Context, workspace, environment string, limit int) ([]AlertEvent, error)
	LatestAlertEvent(ctx context.Context, workspace, environment, ruleID string) (*AlertEvent, error)
	QueryLatencyStats(ctx context.Context, filter QueryFilter) ([]LatencyStat, error)
	QueryErrorStats(ctx context.Context, filter QueryFilter) ([]ErrorStat, error)
}
