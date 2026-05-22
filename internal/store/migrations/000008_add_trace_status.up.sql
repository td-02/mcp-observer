ALTER TABLE traces ADD COLUMN status TEXT NOT NULL DEFAULT 'success';
CREATE INDEX IF NOT EXISTS idx_traces_status ON traces(status);
