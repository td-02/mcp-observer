ALTER TABLE traces ADD COLUMN team_id TEXT NOT NULL DEFAULT 'default';
CREATE INDEX IF NOT EXISTS idx_traces_team_id ON traces(team_id);
