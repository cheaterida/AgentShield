CREATE TABLE IF NOT EXISTS family_groups (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    family_group_id TEXT NOT NULL REFERENCES family_groups(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL DEFAULT '',
    labels TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (status IN ('online','offline','suspicious','degraded','unknown')),
    risk_score REAL NOT NULL DEFAULT 0.0
        CHECK (risk_score >= 0.0 AND risk_score <= 1.0),
    last_heartbeat_at TEXT,
    registered_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_agents_family ON agents(family_group_id);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);

CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    occurred_at TEXT NOT NULL,
    family_group_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    resource_ref TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    attributes TEXT NOT NULL DEFAULT '{}',
    risk_contribution REAL NOT NULL DEFAULT 0.0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_audit_agent_time ON audit_events(agent_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_family_time ON audit_events(family_group_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_events(action);

CREATE TABLE IF NOT EXISTS policy_bundles (
    version TEXT PRIMARY KEY,
    payload BLOB NOT NULL,
    digest TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS risk_alerts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id TEXT NOT NULL UNIQUE,
    family_group_id TEXT NOT NULL,
    agent_id TEXT,
    severity TEXT NOT NULL CHECK (severity IN ('low','medium','high','critical')),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','acknowledged','resolved','dismissed')),
    metadata_json TEXT NOT NULL DEFAULT '{}',
    occurred_at TEXT NOT NULL,
    resolved_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_alerts_family ON risk_alerts(family_group_id);
CREATE INDEX IF NOT EXISTS idx_alerts_severity ON risk_alerts(severity);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON risk_alerts(status);
