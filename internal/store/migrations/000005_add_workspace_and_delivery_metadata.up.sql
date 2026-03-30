ALTER TABLE traces ADD COLUMN workspace TEXT NOT NULL DEFAULT 'default';
ALTER TABLE alert_rules ADD COLUMN workspace TEXT NOT NULL DEFAULT 'default';
ALTER TABLE alert_events ADD COLUMN workspace TEXT NOT NULL DEFAULT 'default';
ALTER TABLE alert_events ADD COLUMN delivery_target TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN delivery_detail TEXT NOT NULL DEFAULT '';
ALTER TABLE alert_events ADD COLUMN delivery_attempts INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_traces_workspace ON traces(workspace);
CREATE INDEX IF NOT EXISTS idx_traces_workspace_environment ON traces(workspace, environment);
CREATE INDEX IF NOT EXISTS idx_alert_rules_workspace ON alert_rules(workspace);
CREATE INDEX IF NOT EXISTS idx_alert_rules_workspace_environment ON alert_rules(workspace, environment);
CREATE INDEX IF NOT EXISTS idx_alert_events_workspace ON alert_events(workspace);
CREATE INDEX IF NOT EXISTS idx_alert_events_workspace_environment ON alert_events(workspace, environment);
