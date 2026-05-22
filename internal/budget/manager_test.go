package budget

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"mcpscope/internal/store"
)

func TestManagerBlocksAndIncrementsUsage(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Budgets: []TeamBudget{
			{
				Team:   "team-alpha",
				Header: "X-Team-ID",
				Limits: BudgetLimits{
					CallsPerHour: 1,
					CallsPerDay:  10,
					TokensPerDay: 100,
				},
			},
		},
	}
	st := newMemoryBudgetStore()
	manager := NewManager(cfg, st)
	now := time.Date(2026, 5, 21, 10, 15, 0, 0, time.UTC)

	first, err := manager.CheckAndReserve(context.Background(), "team-alpha", now)
	if err != nil {
		t.Fatalf("CheckAndReserve returned error: %v", err)
	}
	if !first.Allowed {
		t.Fatalf("expected first request to be allowed: %+v", first)
	}

	if err := manager.RecordTokens(context.Background(), "team-alpha", 42, now); err != nil {
		t.Fatalf("RecordTokens returned error: %v", err)
	}

	second, err := manager.CheckAndReserve(context.Background(), "team-alpha", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("second CheckAndReserve returned error: %v", err)
	}
	if second.Allowed {
		t.Fatalf("expected second request to be blocked: %+v", second)
	}
	if second.Reason != "budget exceeded: calls_per_hour limit reached" {
		t.Fatalf("unexpected block reason: %q", second.Reason)
	}

	hourStart := WindowStart(now, WindowHour)
	dayStart := WindowStart(now, WindowDay)

	hourUsage, err := st.GetBudgetUsage(context.Background(), "team-alpha", string(WindowHour), hourStart)
	if err != nil {
		t.Fatalf("GetBudgetUsage hour returned error: %v", err)
	}
	if hourUsage.CallCount != 1 || hourUsage.TokenCount != 42 {
		t.Fatalf("unexpected hour usage: %+v", hourUsage)
	}

	dayUsage, err := st.GetBudgetUsage(context.Background(), "team-alpha", string(WindowDay), dayStart)
	if err != nil {
		t.Fatalf("GetBudgetUsage day returned error: %v", err)
	}
	if dayUsage.CallCount != 1 || dayUsage.TokenCount != 42 {
		t.Fatalf("unexpected day usage: %+v", dayUsage)
	}
}

type memoryBudgetStore struct {
	mu   sync.Mutex
	rows map[string]store.BudgetUsage
}

func newMemoryBudgetStore() *memoryBudgetStore {
	return &memoryBudgetStore{rows: make(map[string]store.BudgetUsage)}
}

func (m *memoryBudgetStore) ListBudgetUsage(context.Context) ([]store.BudgetUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rows := make([]store.BudgetUsage, 0, len(m.rows))
	for _, row := range m.rows {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TeamID == rows[j].TeamID {
			if rows[i].WindowType == rows[j].WindowType {
				return rows[i].WindowStart.Before(rows[j].WindowStart)
			}
			return rows[i].WindowType < rows[j].WindowType
		}
		return rows[i].TeamID < rows[j].TeamID
	})
	return rows, nil
}

func (m *memoryBudgetStore) IncrementBudgetUsage(_ context.Context, usage store.BudgetUsage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := budgetKey(usage.TeamID, usage.WindowType, usage.WindowStart)
	current := m.rows[key]
	current.TeamID = usage.TeamID
	current.WindowType = usage.WindowType
	current.WindowStart = usage.WindowStart.UTC()
	current.CallCount += usage.CallCount
	current.TokenCount += usage.TokenCount
	m.rows[key] = current
	return nil
}

func (m *memoryBudgetStore) ResetBudgetWindow(_ context.Context, teamID, windowType string, windowStart time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.rows, budgetKey(teamID, windowType, windowStart))
	return nil
}

func (m *memoryBudgetStore) GetBudgetUsage(_ context.Context, teamID, windowType string, windowStart time.Time) (store.BudgetUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := budgetKey(teamID, windowType, windowStart)
	if row, ok := m.rows[key]; ok {
		return row, nil
	}
	return store.BudgetUsage{
		TeamID:      teamID,
		WindowType:  windowType,
		WindowStart: windowStart.UTC(),
	}, nil
}

func budgetKey(teamID, windowType string, windowStart time.Time) string {
	return teamID + "|" + windowType + "|" + windowStart.UTC().Format(time.RFC3339Nano)
}
