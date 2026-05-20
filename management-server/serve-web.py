#!/usr/bin/env python3
"""AgentShield Web 控制台 — SPA 服务 + API 代理 + ClickHouse Trace 查询。

Usage: python serve-web.py [port]

  http://localhost:8081          → Web UI (SPA)
  http://localhost:8081/api/...  → 透明代理到 management-server :8080（除下述例外）

── Trace API 唯一实现位置 ──

serve-web.py 是 Trace API 的**唯一数据源**（ClickHouse agentshield.spans）。
Go 后端 (router.go:42-44) 的 listTraces / listSpans / ingestSpans 端点返回 SQLite
简化格式（不含 prompt/completion），应由 Stream 1 按合约 B 移除。

  POST /api/v1/spans                        → serve-web.py 直接写入 ClickHouse
  GET  /api/v1/traces                       → serve-web.py 直接查询 ClickHouse
  GET  /api/v1/traces/<trace_id>            → serve-web.py 直接查询 ClickHouse
  GET  /api/v1/traces/by-agent              → serve-web.py 直接查询 ClickHouse
  GET  /api/v1/family-groups-with-agents    → serve-web.py 聚合（调 Go API 后组装）

所有其他 /api/* 路径透明代理到 Go :8080（含 POST /api/v1/audit/events — OPA 由 Go 全权负责）。
前端 TraceSpan / TraceGroup 类型以此处返回格式为权威契约（见 stream-3-python-api.md 合约 #10）。

已知微小偏差（非破坏性）：
  - _list_traces / _traces_by_agent SELECT 未包含 parent_id, status_code, agent_id, family_group_id；
    _get_trace_detail 包含 parent_id, status_code 但未含 agent_id, family_group_id。
    前端 TraceSpan 这些字段会收到 undefined，当前页面未使用它们故无影响。
  - CH events 子对象包含 timestamp 字段，前端 SpanEvent 类型未声明（运行时被忽略）。
"""

import http.server
import json
import os
import sys
import urllib.request
import urllib.error
import urllib.parse

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8081
API_BACKEND = "http://localhost:8080"
WEB_DIR = os.path.join(os.path.dirname(__file__), "web", "dist")

CH_HOST = os.environ.get("CLICKHOUSE_HOST", "localhost")
CH_PORT = os.environ.get("CLICKHOUSE_PORT", "8123")
CH_USER = os.environ.get("CLICKHOUSE_USER", "")
CH_PASS = os.environ.get("CLICKHOUSE_PASSWORD", "")
CH_DB = os.environ.get("CLICKHOUSE_DATABASE", "agentshield")
CH_TABLE = os.environ.get("LANGTRACE_PROJECT_ID", "")

# AgentShield own ClickHouse schema (independent of Langtrace)
AS_CH_DB = "agentshield"
AS_SPANS_TABLE = f"{AS_CH_DB}.spans"

OPA_URL = os.environ.get("AGENTSHIELD_OPA_BASE_URL", "http://localhost:8181")
OPA_ENABLED = os.environ.get("AGENTSHIELD_OPA_ENABLED", "1") == "1"


def ch_insert_spans(spans: list[dict]) -> int:
    """Insert spans into AgentShield ClickHouse table via JSONEachRow."""
    import requests as _requests
    if not spans:
        return 0
    lines = []
    for s in spans:
        # Ensure JSON-serializable fields for nested data
        for field in ("attributes", "events", "resource_attributes"):
            val = s.get(field, "")
            if isinstance(val, (dict, list)):
                s[field] = json.dumps(val, ensure_ascii=False)
            elif val is None:
                s[field] = "{}"
        row = {
            "trace_id": s.get("trace_id", ""),
            "span_id": s.get("span_id", ""),
            "parent_id": s.get("parent_id", ""),
            "name": s.get("name", ""),
            "kind": s.get("kind", 0),
            "start_time": s.get("start_time", ""),
            "end_time": s.get("end_time", ""),
            "duration": s.get("duration", 0),
            "status_code": s.get("status_code", 0),
            "status_message": s.get("status_message", ""),
            "attributes": s.get("attributes", "{}"),
            "events": s.get("events", "[]"),
            "resource_attributes": s.get("resource_attributes", "{}"),
            "agent_id": s.get("agent_id", ""),
            "family_group_id": s.get("family_group_id", ""),
            "project_name": s.get("project_name", "agentshield"),
        }
        lines.append(json.dumps(row, ensure_ascii=False))
    body = "\n".join(lines).encode("utf-8")
    url = f"http://{CH_HOST}:{CH_PORT}/?query=INSERT+INTO+{AS_SPANS_TABLE}+FORMAT+JSONEachRow"
    resp = _requests.post(url, auth=(CH_USER, CH_PASS), data=body, timeout=30)
    resp.raise_for_status()
    return len(spans)


def opa_evaluate(path: str, input_data: dict) -> dict:
    """@deprecated — OPA evaluation is now handled by the Go backend (Stream 1).
    Kept for reference only. Do not call from active code paths."""

    import requests as _requests
    try:
        resp = _requests.post(
            f"{OPA_URL}/v1/data/{path}",
            json={"input": input_data},
            timeout=10,
        )
        resp.raise_for_status()
        return resp.json().get("result", {})
    except Exception:
        return {}


def opa_evaluate_audit_events(events: list[dict]) -> list[dict]:
    """@deprecated — OPA evaluation of audit events is now handled by the Go backend (Stream 1).
    Kept for reference only. Do not call from active code paths."""
    if not OPA_ENABLED:
        return []
    alerts = []
    for ev in events:
        destination = ""
        if ev.get("attributes") and isinstance(ev["attributes"], dict):
            destination = ev["attributes"].get("network_dst", "")
        opa_input = {
            "subject": {"family_group_id": ev.get("family_group_id", "")},
            "action": ev.get("action", ""),
            "resource_ref": ev.get("resource_ref", ""),
            "destination": destination,
            "risk_score": ev.get("risk_contribution", 0),
        }
        result = opa_evaluate("agentshield/audit", opa_input)
        if not result:
            continue

        # Inject OPA decision into event attributes
        attrs = ev.get("attributes")
        if not isinstance(attrs, dict):
            attrs = {}
            ev["attributes"] = attrs
        attrs["opa_allow"] = "true" if result.get("allow") else "false"
        attrs["opa_risk_level"] = result.get("risk_level", "")
        if result.get("deny_sensitive_path"):
            attrs["opa_deny_sensitive_path"] = "true"
            attrs["opa_matched_path"] = result.get("matched_sensitive_path", "")
        if result.get("deny_network"):
            attrs["opa_deny_network"] = "true"
        if result.get("risky_write"):
            attrs["opa_risky_write"] = "true"

        # Generate alert for policy violations
        deny_path = result.get("deny_sensitive_path")
        deny_net = result.get("deny_network")
        allowed = result.get("allow")
        risk_level = result.get("risk_level", "")
        if deny_path or deny_net or not allowed:
            severity = "critical" if risk_level == "critical" else "high"
            if deny_path:
                title = "Sensitive path access blocked"
                desc = f"Agent {ev.get('agent_id')} attempted to access {result.get('matched_sensitive_path', 'unknown path')}"
            elif deny_net:
                title = "Restricted network access blocked"
                desc = f"Agent {ev.get('agent_id')} attempted network connection to {destination}"
            else:
                title = "Unauthorized action blocked"
                desc = f"Agent {ev.get('agent_id')} attempted disallowed action: {ev.get('action')}"
            alerts.append({
                "alert_id": f"opa_{ev.get('event_id', 'unknown')}",
                "family_group_id": ev.get("family_group_id", ""),
                "agent_id": ev.get("agent_id", ""),
                "severity": severity,
                "title": title,
                "description": desc,
                "status": "open",
                "occurred_at": ev.get("occurred_at", ""),
            })
    return alerts


def ch_query(sql: str) -> list[dict]:
    formatted = sql.strip()
    if not formatted.upper().endswith("FORMAT JSONEACHROW"):
        formatted += " FORMAT JSONEachRow"
    import requests
    resp = requests.post(
        f"http://{CH_HOST}:{CH_PORT}/",
        auth=(CH_USER, CH_PASS),
        data=formatted.encode("utf-8"),
        timeout=30,
    )
    resp.raise_for_status()
    rows = []
    for line in resp.text.splitlines():
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    return rows


def parse_json_fields(span: dict, fields: list[str]):
    for field in fields:
        raw = span.get(field, "{}")
        if isinstance(raw, str):
            try:
                span[field] = json.loads(raw)
            except (json.JSONDecodeError, TypeError):
                span[field] = {}


class ProxyHandler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=WEB_DIR, **kwargs)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path

        if path.startswith("/api/v1/traces/by-agent"):
            self._traces_by_agent(parsed.query)
        elif path.startswith("/api/v1/traces/"):
            self._get_trace_detail(path)
        elif path == "/api/v1/traces":
            self._list_traces(parsed.query)
        elif path == "/api/v1/family-groups-with-agents":
            self._family_groups_with_agents()
        elif path.startswith("/api/") or path == "/healthz":
            self._proxy()
        else:
            super().do_GET()
            if getattr(self, "_is_404", False):
                self._serve_index()
        self._is_404 = False

    def send_head(self):
        result = super().send_head()
        self._is_404 = (result is None)
        return result

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/api/v1/spans":
            self._ingest_spans()
        else:
            self._proxy()

    def do_PUT(self):
        self._proxy()

    def do_DELETE(self):
        self._proxy()

    def do_OPTIONS(self):
        self.send_response(204)
        self._cors_headers()
        self.end_headers()

    def _cors_headers(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")

    def _proxy(self):
        url = API_BACKEND + self.path
        body = None
        content_len = int(self.headers.get("Content-Length", 0))
        if content_len > 0:
            body = self.rfile.read(content_len)
        req = urllib.request.Request(url, data=body, method=self.command)
        req.add_header("Content-Type", self.headers.get("Content-Type", "application/json"))
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                self.send_response(resp.status)
                self._cors_headers()
                self.send_header("Content-Type", resp.headers.get("Content-Type", "application/json"))
                self.end_headers()
                body = resp.read()
                for field in (b'"recent_alerts":null', b'"alerts":null',
                              b'"agents":null', b'"events":null',
                              b'"bundles":null', b'"groups":null'):
                    body = body.replace(field, field.replace(b":null", b":[]"))
                self.wfile.write(body)
        except urllib.error.HTTPError as e:
            self.send_response(e.code)
            self._cors_headers()
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(e.read())
        except Exception as e:
            self.send_response(502)
            self._cors_headers()
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(f'{{"error":"proxy failed: {e}"}}'.encode())

    def _audit_events_with_opa(self):
        """@deprecated — audit/events is now transparently proxied to Go backend (Stream 1).
        OPA evaluation is the Go backend's sole responsibility per 合约 A.
        Kept for reference only. Do not call from active code paths."""
        content_len = int(self.headers.get("Content-Length", 0))
        if content_len <= 0:
            self._proxy()
            return
        body = self.rfile.read(content_len)
        try:
            payload = json.loads(body)
        except json.JSONDecodeError:
            self._proxy()
            return

        events = payload.get("events", [])
        if events and OPA_ENABLED:
            opa_alerts = opa_evaluate_audit_events(events)
            # Inject OPA alerts into forwarded payload so management-server stores them
            if opa_alerts:
                # Post alerts directly to management-server
                import requests as _requests
                try:
                    for alert in opa_alerts:
                        _requests.post(
                            f"{API_BACKEND}/api/v1/audit/events",
                            json={"events": [{
                                "event_id": alert["alert_id"],
                                "family_group_id": alert["family_group_id"],
                                "agent_id": alert["agent_id"],
                                "resource_ref": "opa:policy_violation",
                                "action": "policy_deny",
                                "attributes": {
                                    "opa_alert_severity": alert["severity"],
                                    "opa_alert_title": alert["title"],
                                    "opa_alert_description": alert["description"],
                                },
                                "risk_contribution": 0.9 if alert["severity"] == "critical" else 0.7,
                                "occurred_at": alert.get("occurred_at", ""),
                            }]},
                            timeout=10,
                        )
                except Exception:
                    pass

        # Forward original body (with OPA-injected attributes) to backend
        modified_body = json.dumps(payload, ensure_ascii=False, default=str).encode("utf-8")
        url = API_BACKEND + self.path
        req = urllib.request.Request(url, data=modified_body, method="POST")
        req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                self.send_response(resp.status)
                self._cors_headers()
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                resp_body = resp.read()
                for field in (b'"recent_alerts":null', b'"alerts":null',
                              b'"agents":null', b'"events":null',
                              b'"bundles":null', b'"groups":null'):
                    resp_body = resp_body.replace(field, field.replace(b":null", b":[]"))
                self.wfile.write(resp_body)
        except urllib.error.HTTPError as e:
            self.send_response(e.code)
            self._cors_headers()
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(e.read())
        except Exception as e:
            self.send_response(502)
            self._cors_headers()
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(f'{{"error":"proxy failed: {e}"}}'.encode())

    def _ingest_spans(self):
        """Ingest spans directly into AgentShield ClickHouse (replaces Langtrace Collector).

        Accepts:
        - OTLP-compatible JSON: {"resourceSpans": [...]}
        - Simple span batch: {"spans": [...]}
        - Single span: {"trace_id": "...", ...}
        """
        content_len = int(self.headers.get("Content-Length", 0))
        if content_len <= 0:
            self._json_resp(400, {"error": "empty body"})
            return
        try:
            payload = json.loads(self.rfile.read(content_len))
        except json.JSONDecodeError as e:
            self._json_resp(400, {"error": f"invalid json: {e}"})
            return

        spans = []
        agent_id = self.headers.get("X-AgentShield-Agent-ID", "")
        family_group_id = self.headers.get("X-AgentShield-Family-Group-ID", "")

        # Parse OTLP format: resourceSpans → scopeSpans → spans
        if "resourceSpans" in payload:
            for rs in payload.get("resourceSpans", []):
                resource_attrs = {}
                for attr in rs.get("resource", {}).get("attributes", []):
                    resource_attrs[attr.get("key", "")] = str(attr.get("value", {}).get("stringValue", ""))
                for ss in rs.get("scopeSpans", []):
                    for s in ss.get("spans", []):
                        spans.append(self._normalize_span(s, agent_id, family_group_id, resource_attrs))
        elif "spans" in payload:
            for s in payload["spans"]:
                spans.append(self._normalize_span(s, agent_id, family_group_id, {}))
        elif "trace_id" in payload:
            spans.append(self._normalize_span(payload, agent_id, family_group_id, {}))
        else:
            self._json_resp(400, {"error": "unrecognized format. Send OTLP resourceSpans, spans array, or single span"})
            return

        if not spans:
            self._json_resp(400, {"error": "no spans parsed"})
            return

        try:
            n = ch_insert_spans(spans)
            self._json_resp(202, {"accepted": n})
        except Exception as e:
            self._json_resp(500, {"error": f"insert failed: {e}"})

    @staticmethod
    def _normalize_span(raw: dict, agent_id: str, family_group_id: str, resource_attrs: dict) -> dict:
        """Normalize a span from OTLP or simple format into AgentShield schema."""
        start_ns = int(raw.get("startTimeUnixNano", 0))
        end_ns = int(raw.get("endTimeUnixNano", 0))
        # ClickHouse DateTime64 expects "YYYY-MM-DD HH:MM:SS.fff" (no T, no Z)
        import datetime as _dt
        if start_ns:
            start_ts = _dt.datetime.fromtimestamp(start_ns / 1e9, tz=_dt.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.") + f"{start_ns % 1_000_000_000 // 1_000:03d}"
        else:
            start_ts = raw.get("start_time", "").replace("T", " ").rstrip("Z")
        if end_ns:
            end_ts = _dt.datetime.fromtimestamp(end_ns / 1e9, tz=_dt.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.") + f"{end_ns % 1_000_000_000 // 1_000:03d}"
        else:
            end_ts = raw.get("end_time", "").replace("T", " ").rstrip("Z")
        duration = end_ns // 1_000_000 - start_ns // 1_000_000 if end_ns and start_ns else 0

        # Extract attributes
        attrs = raw.get("attributes", {})
        if isinstance(attrs, list):  # OTLP format: [{key, value}]
            attrs = {a["key"]: str(a.get("value", {}).get("stringValue", a.get("value", {}))) for a in attrs}

        # Extract events (prompt/completion content)
        events = raw.get("events", [])
        if isinstance(events, list) and events and isinstance(events[0], dict):
            parsed_events = []
            for ev in events:
                ev_attrs = ev.get("attributes", [])
                if isinstance(ev_attrs, list):
                    ev_attrs = {a["key"]: str(a.get("value", {}).get("stringValue", a.get("value", {}))) for a in ev_attrs}
                parsed_events.append({
                    "name": ev.get("name", ""),
                    "timestamp": ev.get("timeUnixNano", 0),
                    "attributes": ev_attrs,
                })
            events = parsed_events if parsed_events else events

        return {
            "trace_id": raw.get("trace_id", raw.get("traceId", "")),
            "span_id": raw.get("span_id", raw.get("spanId", "")),
            "parent_id": raw.get("parent_id", raw.get("parentSpanId", "")),
            "name": raw.get("name", ""),
            "kind": raw.get("kind", 0),
            "start_time": start_ts,
            "end_time": end_ts,
            "duration": duration or raw.get("duration", 0),
            "status_code": raw.get("status", {}).get("code", raw.get("status_code", 0)) if isinstance(raw.get("status"), dict) else 0,
            "status_message": raw.get("status", {}).get("message", "") if isinstance(raw.get("status"), dict) else "",
            "attributes": attrs,
            "events": events,
            "resource_attributes": resource_attrs,
            "agent_id": agent_id or raw.get("agent_id", ""),
            "family_group_id": family_group_id or raw.get("family_group_id", ""),
            "project_name": raw.get("project_name", "agentshield"),
        }

    def _serve_index(self):
        index_path = os.path.join(WEB_DIR, "index.html")
        if os.path.exists(index_path):
            self.send_response(200)
            self._cors_headers()
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            with open(index_path, "rb") as f:
                self.wfile.write(f.read())

    # ── Traces API ──

    def _list_traces(self, query_string: str):
        params = urllib.parse.parse_qs(query_string)
        limit = int(params.get("limit", [20])[0])
        offset = int(params.get("offset", [0])[0])
        try:
            sql = (
                f"SELECT trace_id, span_id, name, start_time, end_time, duration, "
                f"       attributes, events, kind "
                f"FROM {AS_SPANS_TABLE} "
                f"ORDER BY start_time DESC "
                f"LIMIT {limit} OFFSET {offset}"
            )
            rows = ch_query(sql)
            traces: dict[str, list[dict]] = {}
            for span in rows:
                tid = span.get("trace_id", "?")
                if tid not in traces:
                    traces[tid] = []
                parse_json_fields(span, ["attributes", "events"])
                traces[tid].append(span)
            result = {
                "traces": [
                    {
                        "trace_id": tid,
                        "span_count": len(spans),
                        "earliest": min(s["start_time"] for s in spans),
                        "latest": max(s["end_time"] for s in spans),
                        "spans": sorted(spans, key=lambda s: s.get("start_time", "")),
                    }
                    for tid, spans in traces.items()
                ],
                "total": len(traces),
            }
            result["traces"].sort(key=lambda t: t["latest"], reverse=True)
            self._json_resp(200, result)
        except Exception as e:
            self._json_resp(500, {"error": str(e)})

    def _get_trace_detail(self, path: str):
        trace_id = path.rstrip("/").split("/")[-1]
        try:
            sql = (
                f"SELECT span_id, trace_id, parent_id, name, start_time, end_time, "
                f"       duration, kind, status_code, attributes, events "
                f"FROM {AS_SPANS_TABLE} "
                f"WHERE trace_id = '{trace_id}' "
                f"ORDER BY start_time ASC"
            )
            rows = ch_query(sql)
            for span in rows:
                parse_json_fields(span, ["attributes", "events"])
            self._json_resp(200, {"trace_id": trace_id, "spans": rows})
        except Exception as e:
            self._json_resp(500, {"error": str(e)})

    def _traces_by_agent(self, query_string: str):
        """Get traces filtered by agent_id directly from agentshield.spans table."""
        params = urllib.parse.parse_qs(query_string)
        agent_id = params.get("agent_id", [None])[0]
        limit = int(params.get("limit", [20])[0])
        if not agent_id:
            self._json_resp(400, {"error": "agent_id is required"})
            return
        try:
            sql = (
                f"SELECT trace_id, span_id, name, start_time, end_time, duration, "
                f"       attributes, events, kind "
                f"FROM {AS_SPANS_TABLE} "
                f"WHERE agent_id = '{agent_id}' "
                f"ORDER BY start_time DESC "
                f"LIMIT {limit * 10}"
            )
            rows = ch_query(sql)

            # 3. Group by trace_id
            traces_map: dict[str, list[dict]] = {}
            for span in rows:
                tid = span.get("trace_id", "?")
                if tid not in traces_map:
                    traces_map[tid] = []
                parse_json_fields(span, ["attributes", "events"])
                traces_map[tid].append(span)

            result_traces = [
                {
                    "trace_id": tid,
                    "span_count": len(spans),
                    "earliest": min(s["start_time"] for s in spans),
                    "latest": max(s["end_time"] for s in spans),
                    "spans": sorted(spans, key=lambda s: s.get("start_time", "")),
                }
                for tid, spans in traces_map.items()
            ]
            result_traces.sort(key=lambda t: t["latest"], reverse=True)
            # Get family_group_id from the first span
            fgid = ""
            if rows and len(rows) > 0:
                fgid = rows[0].get("family_group_id", "")
            self._json_resp(200, {
                "traces": result_traces[:limit],
                "total": len(result_traces),
                "agent_id": agent_id,
                "family_group_id": fgid,
            })
        except Exception as e:
            self._json_resp(500, {"error": str(e)})

    def _family_groups_with_agents(self):
        """Return family groups with their agents for the sidebar tree."""
        try:
            groups = []
            agents = []
            try:
                req = urllib.request.Request(f"{API_BACKEND}/api/v1/family-groups")
                req.add_header("Accept", "application/json")
                with urllib.request.urlopen(req, timeout=10) as resp:
                    data = json.loads(resp.read())
                groups = data.get("groups") or data.get("family_groups") or []
            except Exception:
                pass

            try:
                req = urllib.request.Request(f"{API_BACKEND}/api/v1/agents")
                req.add_header("Accept", "application/json")
                with urllib.request.urlopen(req, timeout=10) as resp:
                    data = json.loads(resp.read())
                agents = data.get("agents") or []
            except Exception:
                pass

            # Build tree: group → agents
            groups_with_agents = []
            for g in groups:
                gid = g.get("id", "")
                g_agents = [a for a in agents if a.get("family_group_id") == gid]
                groups_with_agents.append({
                    "id": gid,
                    "name": g.get("display_name", gid),
                    "display_name": g.get("display_name", gid),
                    "agent_count": len(g_agents),
                    "agents": [
                        {"id": a["id"], "name": a.get("display_name", a["id"]),
                         "display_name": a.get("display_name", a["id"]),
                         "hostname": a.get("hostname", ""), "status": a.get("status", "unknown")}
                        for a in g_agents
                    ],
                })
            # Also include agents with no group
            orphan_agents = [a for a in agents if not a.get("family_group_id")]
            if orphan_agents:
                groups_with_agents.append({
                    "id": "__ungrouped__",
                    "name": "未分组",
                    "display_name": "未分组",
                    "agent_count": len(orphan_agents),
                    "agents": [
                        {"id": a["id"], "name": a.get("display_name", a["id"]),
                         "display_name": a.get("display_name", a["id"]),
                         "hostname": a.get("hostname", ""), "status": a.get("status", "unknown")}
                        for a in orphan_agents
                    ],
                })

            self._json_resp(200, {"groups": groups_with_agents})
        except Exception as e:
            self._json_resp(500, {"error": str(e)})

    def _json_resp(self, status: int, data: dict):
        self.send_response(status)
        self._cors_headers()
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False, default=str).encode("utf-8"))

if __name__ == "__main__":
    abs_web = os.path.abspath(WEB_DIR)
    if not os.path.isdir(abs_web):
        print(f"ERROR: web/dist not found at {abs_web}")
        print("Run: cd management-server/web && npm install && npm run build")
        sys.exit(1)

    # Auto-detect table if env not set
    if not CH_TABLE:
        import requests as _req
        try:
            detect = _req.post(
                f"http://{CH_HOST}:{CH_PORT}/",
                auth=(CH_USER, CH_PASS),
                data=f"SELECT name FROM {CH_DB}.system.tables WHERE database = '{CH_DB}' AND name NOT LIKE '.%' LIMIT 1 FORMAT JSONEachRow",
                timeout=5,
            )
            rows = [json.loads(l) for l in detect.text.splitlines() if l.strip()]
            if rows:
                CH_TABLE = rows[0]["name"]
                print(f"Auto-detected ClickHouse table: {CH_TABLE}")
        except Exception as e:
            print(f"WARNING: Cannot auto-detect ClickHouse table: {e}")

    server = http.server.HTTPServer(("0.0.0.0", PORT), ProxyHandler)
    print(f"AgentShield Web UI → http://localhost:{PORT}")
    print(f"API proxy         → {API_BACKEND}")
    print("Ctrl+C to stop")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nstopped.")
