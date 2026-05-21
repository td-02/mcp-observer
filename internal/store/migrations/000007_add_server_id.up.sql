ALTER TABLE traces ADD COLUMN server_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_traces_server_id ON traces(server_id);
