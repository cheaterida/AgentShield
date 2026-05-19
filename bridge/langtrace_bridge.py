#!/usr/bin/env python3
"""
AgentShield Span → Audit Event Bridge Service

Polls AgentShield ClickHouse (agentshield.spans) for new spans,
converts each span to an AgentShield audit event, and pushes batches
to the management-server.

Spans originate from AgentShield's own tracer (sdk/python/agentshield_tracer.py)
via POST /api/v1/spans to serve-web.py, which inserts into ClickHouse.

Usage:
  python bridge.py

Environment variables (see .env.example):
  AGENTSHIELD_AGENT_ID       — AgentShield agent identifier (for audit events)
  AGENTSHIELD_FAMILY_GROUP_ID— AgentShield family group (for audit events)
  CLICKHOUSE_HOST            — ClickHouse host (default: localhost)
  CLICKHOUSE_PORT            — ClickHouse HTTP port (default: 8123)
  CLICKHOUSE_USER            — ClickHouse user (no default — must be set)
  CLICKHOUSE_PASSWORD        — ClickHouse password (no default — must be set)
  MANAGEMENT_SERVER_URL      — AgentShield management-server base URL
  POLL_INTERVAL_SECS         — Seconds between polls (default: 30)
  BATCH_SIZE                 — Max spans per poll (default: 100)
"""

import json
import os
import sys
import time
from datetime import datetime, timezone
from typing import Any, Optional

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry


# ── Config ──────────────────────────────────────────────────────────

def _env(key: str, default: str = "") -> str:
    return os.environ.get(key, default).strip()


class Config:
    def __init__(self) -> None:
        self.agent_id = _env("AGENTSHIELD_AGENT_ID")
        self.family_group_id = _env("AGENTSHIELD_FAMILY_GROUP_ID")

        # AgentShield bridge agent — polls spans for THIS agent
        self.bridge_agent_id = _env("AGENTSHIELD_BRIDGE_AGENT_ID", self.agent_id)

        ch_host = _env("CLICKHOUSE_HOST", "localhost")
        ch_port = _env("CLICKHOUSE_PORT", "8123")
        self.clickhouse_url = f"http://{ch_host}:{ch_port}"

        self.clickhouse_user = _env("CLICKHOUSE_USER")
        self.clickhouse_password = _env("CLICKHOUSE_PASSWORD")

        # AgentShield own spans table (independent of Langtrace)
        self.ch_db = "agentshield"
        self.ch_table = f"{self.ch_db}.spans"

        self.mgmt_url = _env("MANAGEMENT_SERVER_URL", "http://localhost:8080")
        self.poll_interval = int(_env("POLL_INTERVAL_SECS", "30"))
        self.batch_size = int(_env("BATCH_SIZE", "100"))

        self._validate()

    def _validate(self) -> None:
        missing = []
        for key, val in [
            ("AGENTSHIELD_AGENT_ID", self.agent_id),
            ("AGENTSHIELD_FAMILY_GROUP_ID", self.family_group_id),
            ("CLICKHOUSE_USER", self.clickhouse_user),
            ("CLICKHOUSE_PASSWORD", self.clickhouse_password),
        ]:
            if not val:
                missing.append(key)
        if missing:
            print(f"[bridge] FATAL — missing env vars: {', '.join(missing)}")
            sys.exit(1)

    def dump(self) -> str:
        return (
            f"table={self.ch_table} agent={self.agent_id} "
            f"fg={self.family_group_id} "
            f"clickhouse={self.clickhouse_url} mgmt={self.mgmt_url} "
            f"poll={self.poll_interval}s batch={self.batch_size}"
        )


# ── ClickHouse client (raw HTTP, zero extra deps) ───────────────────

class ClickHouseClient:
    """Minimal ClickHouse HTTP client."""

    def __init__(self, base_url: str, user: str, password: str) -> None:
        self.base_url = base_url.rstrip("/") + "/"
        self.session = requests.Session()
        self.session.auth = (user, password)
        retry = Retry(total=3, backoff_factor=1, status_forcelist=[502, 503, 504])
        adapter = HTTPAdapter(max_retries=retry)
        self.session.mount("http://", adapter)

    def query(self, sql: str) -> list[dict[str, Any]]:
        """Execute a SELECT and return rows as dicts (JSONEachRow)."""
        # Ensure the query ends with FORMAT JSONEachRow
        formatted = sql.strip()
        if not formatted.upper().endswith("FORMAT JSONEACHROW"):
            formatted += " FORMAT JSONEachRow"

        resp = self.session.post(
            self.base_url,
            data=formatted.encode("utf-8"),
            timeout=30,
        )
        resp.raise_for_status()

        rows: list[dict[str, Any]] = []
        for line in resp.text.splitlines():
            line = line.strip()
            if line:
                rows.append(json.loads(line))
        return rows


# ── Span → AuditEvent converter ─────────────────────────────────────

class SpanConverter:
    """Maps a ClickHouse span row to an AgentShield audit event."""

    # Attribute keys that carry agent-action semantics.
    # agentshield_tracer.py sets both agentshield.service.* (preferred)
    # and langtrace.service.* (legacy compatibility).
    ACTION_KEYS = [
        "agentshield.service.type",
        "langtrace.service.type",
        "agentshield.service.name",
        "langtrace.service.name",
    ]

    @staticmethod
    def extract_action(attrs: dict[str, Any]) -> str:
        for key in SpanConverter.ACTION_KEYS:
            val = attrs.get(key, "")
            if val:
                return str(val)
        return "unknown"

    @staticmethod
    def to_event(
        span: dict[str, Any],
        agent_id: str,
        family_group_id: str,
    ) -> dict[str, Any]:
        attrs: dict[str, Any] = {}
        raw_attrs = span.get("attributes", "{}")
        if isinstance(raw_attrs, str):
            try:
                attrs = json.loads(raw_attrs)
            except (json.JSONDecodeError, TypeError):
                attrs = {"_raw": str(raw_attrs)}
        elif isinstance(raw_attrs, dict):
            attrs = raw_attrs

        action = SpanConverter.extract_action(attrs)

        # Flatten attributes to map[string]string for management-server
        flat_attrs: dict[str, str] = {}
        for k, v in attrs.items():
            if isinstance(v, (dict, list)):
                flat_attrs[k] = json.dumps(v, ensure_ascii=False)
            else:
                flat_attrs[k] = str(v)

        return {
            "event_id": str(span.get("span_id", "")),
            "occurred_at": str(span.get("start_time", "")),
            "family_group_id": family_group_id,
            "agent_id": agent_id,
            "resource_ref": str(span.get("name", "")),
            "action": action,
            "attributes": flat_attrs,
            "risk_contribution": 0.0,
        }


# ── Main bridge loop ─────────────────────────────────────────────────

class SpanBridge:
    def __init__(self, config: Config) -> None:
        self.cfg = config
        self.ch = ClickHouseClient(
            config.clickhouse_url,
            config.clickhouse_user,
            config.clickhouse_password,
        )
        self.converter = SpanConverter()
        self._session = requests.Session()
        self._session.headers["Content-Type"] = "application/json"

    def query_spans(self, since: str) -> list[dict[str, Any]]:
        sql = (
            f"SELECT span_id, trace_id, name, start_time, end_time, "
            f"       attributes, kind, parent_id, status_code, duration "
            f"FROM {self.cfg.ch_table} "
            f"WHERE agent_id = '{self.cfg.bridge_agent_id}' "
            f"  AND start_time > '{since}' "
            f"ORDER BY start_time ASC "
            f"LIMIT {self.cfg.batch_size}"
        )
        return self.ch.query(sql)

    def push_events(self, events: list[dict[str, Any]]) -> int:
        url = f"{self.cfg.mgmt_url}/api/v1/audit/events"
        resp = self._session.post(
            url,
            json={"events": events},
            timeout=15,
        )
        resp.raise_for_status()
        result = resp.json()
        return result.get("accepted", 0)

    def run_once(self, checkpoint: str) -> tuple[str, int]:
        """Single poll cycle. Returns (new_checkpoint, events_pushed)."""
        spans = self.query_spans(checkpoint)
        if not spans:
            return checkpoint, 0

        events = [
            self.converter.to_event(s, self.cfg.agent_id, self.cfg.family_group_id)
            for s in spans
        ]
        accepted = self.push_events(events)
        new_checkpoint = spans[-1].get("start_time", checkpoint)
        return new_checkpoint, accepted

    def run_forever(self) -> None:
        checkpoint = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
        print(f"[bridge] START — {self.cfg.dump()}")

        print(f"[bridge] START — {self.cfg.dump()}")
        print(f"[bridge] checkpoint={checkpoint}")

        while True:
            try:
                new_ck, count = self.run_once(checkpoint)
                if count > 0:
                    print(f"[bridge] pushed {count} events, checkpoint={new_ck}")
                checkpoint = new_ck
            except requests.exceptions.ConnectionError as e:
                print(f"[bridge] connection error — is management-server up? ({e})")
            except requests.exceptions.HTTPError as e:
                print(f"[bridge] HTTP error — {e}")
            except Exception as e:
                print(f"[bridge] unexpected error: {e}")

            time.sleep(self.cfg.poll_interval)


# ── Entry point ──────────────────────────────────────────────────────

def main() -> None:
    cfg = Config()
    bridge = SpanBridge(cfg)
    bridge.run_forever()


if __name__ == "__main__":
    main()
