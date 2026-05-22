package budget

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Budgets []TeamBudget `yaml:"budgets" json:"budgets"`
}

type TeamBudget struct {
	Team   string       `yaml:"team" json:"team"`
	Header string       `yaml:"header" json:"header"`
	Limits BudgetLimits `yaml:"limits" json:"limits"`
}

type BudgetLimits struct {
	CallsPerHour int64 `yaml:"calls_per_hour" json:"calls_per_hour"`
	CallsPerDay  int64 `yaml:"calls_per_day" json:"calls_per_day"`
	TokensPerDay int64 `yaml:"tokens_per_day" json:"tokens_per_day"`
}

type WindowType string

const (
	WindowHour WindowType = "hour"
	WindowDay  WindowType = "day"
)

func LoadConfig(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read budgets config: %w", err)
	}
	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode budgets config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c Config) Validate() error {
	seenTeams := make(map[string]struct{}, len(c.Budgets))
	for i, budget := range c.Budgets {
		if strings.TrimSpace(budget.Team) == "" {
			return fmt.Errorf("budgets[%d].team is required", i)
		}
		if strings.TrimSpace(budget.Header) == "" {
			return fmt.Errorf("budgets[%d].header is required", i)
		}
		if _, ok := seenTeams[budget.Team]; ok {
			return fmt.Errorf("budgets[%d].team duplicates %q", i, budget.Team)
		}
		seenTeams[budget.Team] = struct{}{}
		if budget.Limits.CallsPerHour < 0 {
			return fmt.Errorf("budgets[%d].limits.calls_per_hour must be 0 or greater", i)
		}
		if budget.Limits.CallsPerDay < 0 {
			return fmt.Errorf("budgets[%d].limits.calls_per_day must be 0 or greater", i)
		}
		if budget.Limits.TokensPerDay < 0 {
			return fmt.Errorf("budgets[%d].limits.tokens_per_day must be 0 or greater", i)
		}
	}
	return nil
}

func (c Config) TeamBudget(teamID string) (TeamBudget, bool) {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return TeamBudget{}, false
	}
	for _, budget := range c.Budgets {
		if strings.TrimSpace(budget.Team) == teamID {
			return budget, true
		}
	}
	return TeamBudget{}, false
}

func WindowStart(now time.Time, window WindowType) time.Time {
	now = now.UTC()
	switch window {
	case WindowHour:
		return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
	case WindowDay:
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return now
	}
}
