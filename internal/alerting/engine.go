package alerting

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"mcpscope/internal/store"
)

type Options struct {
	Workspace   string
	Environment string
	PublicURL   string
	Logger      io.Writer
}

type Engine struct {
	store       store.TraceStore
	cfg         Config
	workspace   string
	environment string
	publicURL   string
	logger      io.Writer

	mu        sync.RWMutex
	lastFired map[string]time.Time
	rules     []compiledRule
}

type compiledRule struct {
	RuleConfig
	condition Condition
}

func NewEngine(cfg Config, traceStore store.TraceStore, opts Options) (*Engine, error) {
	if traceStore == nil {
		return nil, fmt.Errorf("trace storage unavailable")
	}

	engine := &Engine{
		store:       traceStore,
		cfg:         cfg,
		workspace:   defaultScopeValue(opts.Workspace),
		environment: defaultScopeValue(opts.Environment),
		publicURL:   strings.TrimSpace(opts.PublicURL),
		logger:      opts.Logger,
		lastFired:   make(map[string]time.Time),
	}

	for _, rule := range cfg.Rules {
		parsed, err := ParseCondition(rule.Condition)
		if err != nil {
			return nil, err
		}
		engine.rules = append(engine.rules, compiledRule{RuleConfig: rule, condition: parsed})
	}

	return engine, nil
}

func defaultScopeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}

func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	e.evaluateOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.evaluateOnce(ctx)
		}
	}
}

func (e *Engine) Snapshot() []RuleStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]RuleStatus, 0, len(e.rules))
	for _, rule := range e.rules {
		status := RuleStatus{
			Name:      rule.Name,
			Condition: rule.Condition,
			Channels:  append([]string(nil), rule.Channels...),
		}
		if firedAt, ok := e.lastFired[rule.Name]; ok {
			value := firedAt.UTC()
			status.LastFiredAt = &value
		}
		out = append(out, status)
	}
	return out
}

func (e *Engine) evaluateOnce(ctx context.Context) {
	now := time.Now().UTC()
	start := now.Add(-5 * time.Minute)

	traces, err := e.store.Query(ctx, store.QueryFilter{
		Workspace:    e.workspace,
		Environment:  e.environment,
		CreatedAfter: &start,
	})
	if err != nil {
		e.logf("alert evaluation failed: %v", err)
		return
	}

	for _, rule := range e.rules {
		result, fired, err := EvaluateCondition(rule.Condition, traces)
		if err != nil {
			e.logf("alert rule %q evaluation failed: %v", rule.Name, err)
			continue
		}
		if !fired {
			continue
		}

		if !e.shouldFire(rule.Name, now) {
			continue
		}
		e.setLastFired(rule.Name, now)
		e.dispatch(ctx, rule.RuleConfig, result, now)
	}
}

func (e *Engine) shouldFire(name string, now time.Time) bool {
	e.mu.RLock()
	last, ok := e.lastFired[name]
	e.mu.RUnlock()
	if !ok {
		return true
	}
	return now.Sub(last) >= 15*time.Minute
}

func (e *Engine) setLastFired(name string, now time.Time) {
	e.mu.Lock()
	e.lastFired[name] = now
	e.mu.Unlock()
}

func (e *Engine) dispatch(ctx context.Context, rule RuleConfig, result ConditionResult, firedAt time.Time) {
	for _, channel := range rule.Channels {
		switch strings.ToLower(strings.TrimSpace(channel)) {
		case "slack":
			sender := SlackSender{WebhookURL: e.cfg.Slack.WebhookURL, PublicURL: e.publicURL}
			if err := sender.Send(ctx, rule, result, firedAt); err != nil {
				e.logf("slack alert for %q failed: %v", rule.Name, err)
			}
		case "pagerduty":
			sender := PagerDutySender{RoutingKey: e.cfg.PagerDuty.RoutingKey, PublicURL: e.publicURL}
			if err := sender.Send(ctx, rule, result, firedAt); err != nil {
				e.logf("pagerduty alert for %q failed: %v", rule.Name, err)
			}
		}
	}
}

func (e *Engine) logf(format string, args ...any) {
	if e.logger == nil {
		return
	}
	_, _ = fmt.Fprintf(e.logger, format+"\n", args...)
}

func percentile(values []int64, pct float64) int64 {
	if len(values) == 0 {
		return 0
	}
	if pct <= 0 {
		return values[0]
	}
	if pct >= 1 {
		return values[len(values)-1]
	}
	index := int(math.Ceil(float64(len(values))*pct)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
