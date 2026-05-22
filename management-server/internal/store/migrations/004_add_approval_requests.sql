CREATE TABLE IF NOT EXISTS approval_requests (
    request_id      TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    family_group_id TEXT NOT NULL,
    action          TEXT NOT NULL DEFAULT '',
    resource_ref    TEXT NOT NULL DEFAULT '',
    risk_score      REAL NOT NULL DEFAULT 0,
    tier            TEXT NOT NULL DEFAULT 'department',
    status          TEXT NOT NULL DEFAULT 'pending',
    requested_by    TEXT NOT NULL DEFAULT '',
    approved_by     TEXT NOT NULL DEFAULT '',
    approved_at     TEXT,
    expires_at      TEXT,
    metadata_json   TEXT DEFAULT '{}',
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_approval_fg ON approval_requests(family_group_id, status);
CREATE INDEX IF NOT EXISTS idx_approval_status ON approval_requests(status);
