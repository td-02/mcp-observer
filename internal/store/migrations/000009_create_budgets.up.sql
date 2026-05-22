CREATE TABLE IF NOT EXISTS budgets (
    team_id TEXT NOT NULL,
    window_start TIMESTAMP NOT NULL,
    window_type TEXT NOT NULL,
    call_count INTEGER NOT NULL DEFAULT 0,
    token_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (team_id, window_start, window_type)
);

CREATE INDEX IF NOT EXISTS idx_budgets_team_id ON budgets(team_id);
CREATE INDEX IF NOT EXISTS idx_budgets_window_type ON budgets(window_type);
CREATE INDEX IF NOT EXISTS idx_budgets_window_start ON budgets(window_start);
