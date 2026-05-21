-- Token 配额定义表
CREATE TABLE IF NOT EXISTS token_quotas (
    quota_id        TEXT PRIMARY KEY,
    target_type     TEXT NOT NULL,       -- 'agent' | 'family_group'
    target_id       TEXT NOT NULL,
    quota_name      TEXT NOT NULL DEFAULT 'default',
    daily_limit     INTEGER NOT NULL DEFAULT -1,    -- -1 表示无限
    weekly_limit    INTEGER NOT NULL DEFAULT -1,
    monthly_limit   INTEGER NOT NULL DEFAULT -1,
    total_limit     INTEGER NOT NULL DEFAULT -1,
    per_request_limit INTEGER NOT NULL DEFAULT -1,
    max_concurrency   INTEGER NOT NULL DEFAULT -1,  -- -1 不限制
    warn_threshold    REAL NOT NULL DEFAULT 0.8,
    block_threshold   REAL NOT NULL DEFAULT 1.0,
    priority          INTEGER NOT NULL DEFAULT 5,   -- 1-10, 10 最高
    active            INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Token 用量日志表
CREATE TABLE IF NOT EXISTS token_usage_logs (
    log_id          TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    family_group_id TEXT NOT NULL,
    span_id         TEXT NOT NULL,
    trace_id        TEXT NOT NULL DEFAULT '',
    model_name      TEXT NOT NULL,
    provider        TEXT NOT NULL DEFAULT '',
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens    INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens   INTEGER NOT NULL DEFAULT 0,
    cost_millicents INTEGER NOT NULL DEFAULT 0,
    quota_status    TEXT NOT NULL DEFAULT 'ok',   -- ok | warned | blocked
    occurred_at     TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_usage_logs_agent ON token_usage_logs(agent_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_usage_logs_fg ON token_usage_logs(family_group_id, occurred_at);

-- 聚合用量表
CREATE TABLE IF NOT EXISTS token_usage_summary (
    summary_id      TEXT PRIMARY KEY,
    target_type     TEXT NOT NULL,
    target_id       TEXT NOT NULL,
    period          TEXT NOT NULL,       -- 'daily' | 'weekly' | 'monthly' | 'total'
    date_key        TEXT NOT NULL,       -- '2026-05-19' | '2026-W21' | '2026-05' | 'total'
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    request_count   INTEGER NOT NULL DEFAULT 0,
    cost_millicents INTEGER NOT NULL DEFAULT 0,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_summary_unique ON token_usage_summary(target_type, target_id, period, date_key);

-- 模型定价表
CREATE TABLE IF NOT EXISTS model_prices (
    model_id        TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    input_price_millicents  INTEGER NOT NULL DEFAULT 0,
    output_price_millicents INTEGER NOT NULL DEFAULT 0,
    cache_read_price_millicents  INTEGER NOT NULL DEFAULT 0,
    active          INTEGER NOT NULL DEFAULT 1,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 预置常用模型定价
INSERT OR IGNORE INTO model_prices (model_id, provider, display_name, input_price_millicents, output_price_millicents) VALUES
    ('gpt-4o',              'openai',    'GPT-4o',               250000, 1000000),
    ('gpt-4o-mini',         'openai',    'GPT-4o Mini',           15000,   60000),
    ('deepseek-chat',       'deepseek',  'DeepSeek-V3',           27000,  110000),
    ('deepseek-reasoner',   'deepseek',  'DeepSeek-R1',           55000,  219000),
    ('claude-opus-4-7',     'anthropic', 'Claude Opus 4.7',     1500000, 7500000),
    ('claude-sonnet-4-6',   'anthropic', 'Claude Sonnet 4.6',    300000, 1500000),
    ('claude-haiku-4-5',    'anthropic', 'Claude Haiku 4.5',      80000,  400000),
    ('qwen-turbo',          'alibaba',   'Qwen Turbo',             3000,   60000);
