-- AgentShield ClickHouse span schema
-- Run against ClickHouse before starting bridge or serve-web.py:
--   clickhouse-client < observability/schemas/clickhouse_spans.sql
-- Or via HTTP:
--   curl -X POST 'http://localhost:8123/' --data-binary @observability/schemas/clickhouse_spans.sql

CREATE DATABASE IF NOT EXISTS agentshield;

CREATE TABLE IF NOT EXISTS agentshield.spans (
    trace_id          String,
    span_id           String,
    parent_id         String,
    name              String,
    kind              Int32,
    start_time        DateTime64(3),
    end_time          DateTime64(3),
    duration          Int64,
    status_code       Int32,
    status_message    String,
    attributes        String,
    events            String,
    resource_attributes String,
    agent_id          String,
    family_group_id   String,
    project_name      String,
    ingested_at       DateTime64(3) DEFAULT now64(3)
) ENGINE = MergeTree()
ORDER BY (family_group_id, agent_id, start_time)
PARTITION BY toYYYYMM(start_time);
