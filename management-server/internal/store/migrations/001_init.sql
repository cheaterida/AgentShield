CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    family_group_id TEXT NOT NULL,
    display_name TEXT DEFAULT '',
    labels TEXT DEFAULT '{}',
    status TEXT DEFAULT 'unknown',
    risk_score REAL DEFAULT 0,
    last_heartbeat_at TEXT,
    registered_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS family_groups (
    id TEXT PRIMARY KEY,
    display_name TEXT DEFAULT '',
    member_principal_ids TEXT DEFAULT '[]',
    labels TEXT DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    event_id TEXT PRIMARY KEY,
    occurred_at TEXT NOT NULL,
    family_group_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    resource_ref TEXT DEFAULT '',
    action TEXT DEFAULT '',
    attributes TEXT DEFAULT '{}',
    risk_contribution REAL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS policy_bundles (
    version TEXT PRIMARY KEY,
    policy_type TEXT DEFAULT 'opa_rego',
    payload BLOB NOT NULL,
    digest TEXT DEFAULT '',
    active INTEGER DEFAULT 0,
    metadata_json TEXT DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS risk_alerts (
    alert_id TEXT PRIMARY KEY,
    family_group_id TEXT NOT NULL,
    agent_id TEXT DEFAULT '',
    severity TEXT NOT NULL,
    title TEXT DEFAULT '',
    description TEXT DEFAULT '',
    status TEXT DEFAULT 'open',
    metadata_json TEXT DEFAULT '{}',
    occurred_at TEXT NOT NULL,
    resolved_at TEXT,
    created_at TEXT NOT NULL
);
