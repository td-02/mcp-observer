package budget

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"mcpscope/internal/store"
)

type Store interface {
	GetBudgetUsage(ctx context.Context, teamID, windowType string, windowStart time.Time) (store.BudgetUsage, error)
	ListBudgetUsage(ctx context.Context) ([]store.BudgetUsage, error)
	IncrementBudgetUsage(ctx context.Context, usage store.BudgetUsage) error
	ResetBudgetWindow(ctx context.Context, teamID, windowType string, windowStart time.Time) error
}

type Decision struct {
	TeamID       string
	Budget       TeamBudget
	Allowed      bool
	Reason       string
	LimitName    string
	WindowType   WindowType
	CurrentUsage store.BudgetUsage
}

type Snapshot struct {
	TeamID      string            `json:"team_id"`
	Header      string            `json:"header"`
	WindowType  WindowType        `json:"window_type"`
	WindowStart time.Time         `json:"window_start"`
	Usage       store.BudgetUsage `json:"usage"`
	Limits      BudgetLimits      `json:"limits"`
}

type Manager struct {
	cfg   *Config
	store Store
	mu    sync.Mutex
}

func NewManager(cfg *Config, st Store) *Manager {
	if cfg == nil || st == nil {
		return nil
	}
	return &Manager{cfg: cfg, store: st}
}

func (m *Manager) CheckAndReserve(ctx context.Context, teamID string, now time.Time) (Decision, error) {
	if m == nil || m.cfg == nil || m.store == nil {
		return Decision{TeamID: "default", Allowed: true}, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	effective := m.teamBudget(teamID)
	decision := Decision{
		TeamID:  teamIDForBudget(effective),
		Budget:  effective,
		Allowed: true,
	}

	windows := []struct {
		typ   WindowType
		limit int64
		name  string
	}{
		{typ: WindowHour, limit: effective.Limits.CallsPerHour, name: "calls_per_hour"},
		{typ: WindowDay, limit: effective.Limits.CallsPerDay, name: "calls_per_day"},
	}
	for _, window := range windows {
		if window.limit <= 0 {
			continue
		}
		start := WindowStart(now, window.typ)
		usage, err := m.store.GetBudgetUsage(ctx, decision.TeamID, string(window.typ), start)
		if err != nil {
			return Decision{}, err
		}
		if usage.CallCount+1 > window.limit {
			decision.Allowed = false
			decision.Reason = fmt.Sprintf("budget exceeded: %s limit reached", window.name)
			decision.LimitName = window.name
			decision.WindowType = window.typ
			decision.CurrentUsage = usage
			return decision, nil
		}
	}

	if effective.Limits.TokensPerDay > 0 {
		start := WindowStart(now, WindowDay)
		usage, err := m.store.GetBudgetUsage(ctx, decision.TeamID, string(WindowDay), start)
		if err != nil {
			return Decision{}, err
		}
		if usage.TokenCount >= effective.Limits.TokensPerDay {
			decision.Allowed = false
			decision.Reason = "budget exceeded: tokens_per_day limit reached"
			decision.LimitName = "tokens_per_day"
			decision.WindowType = WindowDay
			decision.CurrentUsage = usage
			return decision, nil
		}
	}

	for _, window := range []WindowType{WindowHour, WindowDay} {
		if err := m.store.IncrementBudgetUsage(ctx, store.BudgetUsage{
			TeamID:      decision.TeamID,
			WindowType:  string(window),
			WindowStart: WindowStart(now, window),
			CallCount:   1,
		}); err != nil {
			return Decision{}, err
		}
	}

	return decision, nil
}

func (m *Manager) RecordTokens(ctx context.Context, teamID string, tokens int64, now time.Time) error {
	if m == nil || m.cfg == nil || m.store == nil || tokens <= 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	effective := m.teamBudget(teamID)
	team := teamIDForBudget(effective)
	for _, window := range []WindowType{WindowHour, WindowDay} {
		if err := m.store.IncrementBudgetUsage(ctx, store.BudgetUsage{
			TeamID:      team,
			WindowType:  string(window),
			WindowStart: WindowStart(now, window),
			TokenCount:  tokens,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Reset(ctx context.Context, teamID string, window WindowType, now time.Time) error {
	if m == nil || m.cfg == nil || m.store == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		teamID = "default"
	}
	return m.store.ResetBudgetWindow(ctx, teamID, string(window), WindowStart(now, window))
}

func (m *Manager) Snapshot(ctx context.Context, now time.Time) ([]Snapshot, error) {
	if m == nil || m.cfg == nil || m.store == nil {
		return nil, nil
	}

	rows, err := m.store.ListBudgetUsage(ctx)
	if err != nil {
		return nil, err
	}

	currentStarts := map[WindowType]time.Time{
		WindowHour: WindowStart(now, WindowHour),
		WindowDay:  WindowStart(now, WindowDay),
	}
	current := map[string]store.BudgetUsage{}
	for _, row := range rows {
		current[snapshotKey(row.TeamID, row.WindowType, row.WindowStart)] = row
	}

	snapshots := make([]Snapshot, 0, len(current)+len(m.cfg.Budgets)*2)
	added := map[string]struct{}{}

	for _, budget := range m.cfg.Budgets {
		teamID := strings.TrimSpace(budget.Team)
		if teamID == "" {
			continue
		}
		for _, window := range []WindowType{WindowHour, WindowDay} {
			start := WindowStart(now, window)
			key := snapshotKey(teamID, string(window), start)
			usage, ok := current[key]
			if !ok {
				usage = store.BudgetUsage{
					TeamID:      teamID,
					WindowType:  string(window),
					WindowStart: start,
				}
			}
			snapshots = append(snapshots, Snapshot{
				TeamID:      teamID,
				Header:      budget.Header,
				WindowType:  window,
				WindowStart: start,
				Usage:       usage,
				Limits:      budget.Limits,
			})
			added[key] = struct{}{}
		}
	}

	for _, usage := range rows {
		windowType := WindowType(usage.WindowType)
		start, ok := currentStarts[windowType]
		if !ok || !usage.WindowStart.Equal(start) {
			continue
		}
		key := snapshotKey(usage.TeamID, usage.WindowType, usage.WindowStart)
		if _, ok := added[key]; ok {
			continue
		}
		snapshots = append(snapshots, Snapshot{
			TeamID:      usage.TeamID,
			WindowType:  windowType,
			WindowStart: usage.WindowStart,
			Usage:       usage,
		})
	}

	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].TeamID == snapshots[j].TeamID {
			if snapshots[i].WindowType == snapshots[j].WindowType {
				return snapshots[i].WindowStart.Before(snapshots[j].WindowStart)
			}
			return snapshots[i].WindowType < snapshots[j].WindowType
		}
		return snapshots[i].TeamID < snapshots[j].TeamID
	})

	return snapshots, nil
}

func (m *Manager) teamBudget(teamID string) TeamBudget {
	if m == nil || m.cfg == nil {
		return TeamBudget{Team: "default"}
	}
	budget, ok := m.cfg.TeamBudget(teamID)
	if ok {
		return budget
	}
	return TeamBudget{Team: "default"}
}

func teamIDForBudget(budget TeamBudget) string {
	if strings.TrimSpace(budget.Team) == "" {
		return "default"
	}
	return strings.TrimSpace(budget.Team)
}

func snapshotKey(teamID, windowType string, windowStart time.Time) string {
	return teamID + "|" + windowType + "|" + windowStart.UTC().Format(time.RFC3339Nano)
}
