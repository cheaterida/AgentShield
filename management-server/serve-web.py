#!/usr/bin/env python3
"""Serve management-server Web UI + API proxy + ClickHouse trace queries.

Usage: python serve-web.py [port]

  http://localhost:8081          → Web UI (SPA)
  http://localhost:8081/api/...  → Proxied to management-server :8080
  http://localhost:8081/traces   → Trace viewer with input/output
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
    """Query OPA for policy evaluation. Returns the 'result' dict or empty dict on error."""
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
    """Evaluate audit events against OPA policy. Returns policy violation alerts."""
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

        if path == "/traces" or path == "/traces/":
            self._serve_traces_page()
        elif path.startswith("/api/v1/traces/by-agent"):
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
        if parsed.path == "/api/v1/audit/events":
            self._audit_events_with_opa()
        elif parsed.path == "/api/v1/spans":
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
        """Intercept audit event POST, evaluate with OPA, inject results, forward to backend."""
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
                    "name": g.get("name", gid),
                    "agent_count": len(g_agents),
                    "agents": [
                        {"id": a["id"], "name": a.get("name", a["id"]),
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
                    "agent_count": len(orphan_agents),
                    "agents": [
                        {"id": a["id"], "name": a.get("name", a["id"]),
                         "hostname": a.get("hostname", ""), "status": a.get("status", "unknown")}
                        for a in orphan_agents
                    ],
                })

            self._json_resp(200, {"groups": groups_with_agents})
        except Exception as e:
            self._json_resp(500, {"error": str(e)})

    def _serve_traces_page(self):
        html = TRACES_HTML
        self.send_response(200)
        self._cors_headers()
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.end_headers()
        self.wfile.write(html.encode("utf-8"))

    def _json_resp(self, status: int, data: dict):
        self.send_response(status)
        self._cors_headers()
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False, default=str).encode("utf-8"))


# ── Trace viewer HTML ──

TRACES_HTML = r"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Traces — AgentShield</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif; background: #f1f5f9; color: #1e293b; display: flex; height: 100vh; overflow: hidden; }
/* ── Sidebar ── */
.sidebar { width: 280px; min-width: 280px; background: #0f172a; color: #e2e8f0; display: flex; flex-direction: column; overflow: hidden; }
.sidebar-header { padding: 16px 16px 12px; border-bottom: 1px solid #1e293b; }
.sidebar-header h2 { font-size: 16px; font-weight: 700; }
.sidebar-header a { color: #93c5fd; font-size: 12px; text-decoration: none; }
.sidebar-header a:hover { text-decoration: underline; }
.sidebar-nav { flex: 1; overflow-y: auto; padding: 8px 0; }
.sidebar-nav::-webkit-scrollbar { width: 4px; }
.sidebar-nav::-webkit-scrollbar-thumb { background: #334155; border-radius: 2px; }
.nav-all { display: flex; align-items: center; gap: 8px; padding: 10px 16px; cursor: pointer; font-size: 13px; font-weight: 600; color: #e2e8f0; border-left: 3px solid transparent; transition: all 0.15s; }
.nav-all:hover { background: #1e293b; }
.nav-all.active { background: #1e293b; border-left-color: #6366f1; color: #a5b4fc; }
.group-item { margin-top: 4px; }
.group-header { display: flex; align-items: center; gap: 8px; padding: 10px 16px; cursor: pointer; font-size: 13px; font-weight: 600; color: #94a3b8; transition: all 0.15s; }
.group-header:hover { background: #1e293b; color: #cbd5e1; }
.group-arrow { font-size: 10px; width: 12px; text-align: center; transition: transform 0.2s; flex-shrink: 0; }
.group-item.open .group-arrow { transform: rotate(90deg); }
.group-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.group-count { font-size: 11px; color: #64748b; }
.agent-list { display: none; }
.group-item.open .agent-list { display: block; }
.agent-item { display: flex; align-items: center; gap: 8px; padding: 8px 16px 8px 40px; cursor: pointer; font-size: 13px; color: #94a3b8; border-left: 3px solid transparent; transition: all 0.15s; }
.agent-item:hover { background: #1e293b; color: #e2e8f0; }
.agent-item.active { background: #1e293b; border-left-color: #6366f1; color: #c7d2fe; }
.agent-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.agent-dot.online { background: #22c55e; }
.agent-dot.offline { background: #ef4444; }
.agent-dot.unknown { background: #64748b; }
.agent-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.loading-tree { padding: 20px 16px; font-size: 13px; color: #64748b; }
/* ── Main ── */
.main { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
.main-header { background: #fff; padding: 14px 24px; display: flex; align-items: center; justify-content: space-between; border-bottom: 1px solid #e2e8f0; }
.main-header .title { font-size: 16px; font-weight: 700; color: #1e293b; }
.main-header .subtitle { font-size: 12px; color: #64748b; margin-left: 8px; }
.header-right { display: flex; align-items: center; gap: 12px; }
.header-right a { color: #6366f1; font-size: 13px; text-decoration: none; }
.header-right a:hover { text-decoration: underline; }
.content { flex: 1; overflow-y: auto; padding: 20px 24px; }
.content::-webkit-scrollbar { width: 6px; }
.content::-webkit-scrollbar-thumb { background: #cbd5e1; border-radius: 3px; }
/* ── Trace Cards ── */
.trace-card { background: #fff; border-radius: 10px; margin-bottom: 10px; box-shadow: 0 1px 3px rgba(0,0,0,0.06); overflow: hidden; }
.trace-header { padding: 12px 18px; display: flex; align-items: center; justify-content: space-between; cursor: pointer; border-bottom: 1px solid #f1f5f9; }
.trace-header:hover { background: #f8fafc; }
.trace-header .left { display: flex; align-items: center; gap: 10px; flex: 1; min-width: 0; }
.trace-header .right { font-size: 12px; color: #64748b; flex-shrink: 0; }
.trace-id { font-family: 'SF Mono','Consolas',monospace; font-size: 12px; color: #6366f1; }
.span-count { background: #e2e8f0; padding: 2px 8px; border-radius: 10px; font-size: 11px; font-weight: 600; flex-shrink: 0; }
.span-list { display: none; }
.trace-card.open .span-list { display: block; }
.span-item { padding: 10px 18px; border-bottom: 1px solid #f8fafc; cursor: pointer; }
.span-item:hover { background: #f8fafc; }
.span-item.expanded { background: #fffbeb; }
.span-row { display: flex; align-items: center; gap: 10px; font-size: 13px; flex-wrap: wrap; }
.span-name { font-weight: 600; min-width: 180px; }
.span-duration { font-size: 12px; color: #f59e0b; min-width: 70px; }
.span-time { font-size: 11px; color: #94a3b8; min-width: 160px; }
.span-tokens { font-size: 11px; color: #10b981; min-width: 100px; }
.span-model { font-size: 11px; color: #6366f1; }
.span-attrs { display: flex; flex-wrap: wrap; gap: 4px; flex: 1; }
.attr-tag { background: #f1f5f9; padding: 1px 6px; border-radius: 3px; font-size: 10px; color: #475569; white-space: nowrap; }
.span-detail { display: none; margin-top: 10px; padding-top: 10px; border-top: 1px solid #e2e8f0; }
.span-item.expanded .span-detail { display: block; }
.content-block { margin-bottom: 12px; }
.content-block h4 { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; color: #64748b; margin-bottom: 5px; }
.content-block pre { background: #1e293b; color: #e2e8f0; padding: 12px; border-radius: 6px; font-size: 11px; line-height: 1.5; max-height: 350px; overflow-y: auto; white-space: pre-wrap; word-break: break-word; }
.msg-item { margin-bottom: 8px; padding: 8px; background: #f8fafc; border-radius: 6px; border-left: 3px solid #6366f1; }
.msg-content { font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-word; color: #334155; }
.tool-call { margin-left: 12px; padding: 4px 8px; background: #fef3c7; border-radius: 4px; font-size: 11px; margin-top: 3px; }
.role { display: inline-block; padding: 1px 6px; border-radius: 3px; font-size: 10px; font-weight: 600; margin-bottom: 3px; }
.role-system { background: #fce7f3; color: #be185d; }
.role-user { background: #dbeafe; color: #1d4ed8; }
.role-assistant { background: #d1fae5; color: #047857; }
.role-tool { background: #fef3c7; color: #b45309; }
.badge { display: inline-block; padding: 1px 6px; border-radius: 3px; font-size: 10px; font-weight: 600; }
.badge-llm { background: #ede9fe; color: #7c3aed; }
.badge-framework { background: #dbeafe; color: #2563eb; }
.badge-vectordb { background: #fce7f3; color: #db2777; }
.badge-other { background: #f1f5f9; color: #64748b; }
.expand-hint { font-size: 10px; color: #94a3b8; }
.state-msg { text-align: center; padding: 48px 20px; color: #94a3b8; }
.state-msg.loading { }
.state-msg.empty { }
.state-msg.error { background: #fef2f2; color: #dc2626; border-radius: 8px; margin: 16px 0; }
.state-msg small { display: block; margin-top: 6px; font-size: 12px; }
.spinner { display: inline-block; width: 20px; height: 20px; border: 2px solid #e2e8f0; border-top-color: #6366f1; border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>
<!-- ── Sidebar ── -->
<div class="sidebar">
  <div class="sidebar-header">
    <h2>🔍 链路追踪</h2>
    <a href="/">← 返回仪表盘</a>
  </div>
  <div class="sidebar-nav" id="sidebarNav">
    <div class="loading-tree">加载中...</div>
  </div>
</div>
<!-- ── Main content ── -->
<div class="main">
  <div class="main-header">
    <div>
      <span class="title" id="contentTitle">全部 Traces</span>
      <span class="subtitle" id="contentSubtitle"></span>
    </div>
    <div class="header-right">
      <a href="/">仪表盘</a>
      <a href="/audit-log">审计日志</a>
    </div>
  </div>
  <div class="content" id="app">
    <div class="state-msg loading"><div class="spinner"></div><br><small>加载中...</small></div>
  </div>
</div>
<script>
const BASE = '/api/v1';
var currentAgentId = null;
var currentFamilyGroupId = null;
var allAgents = [];

// ── Sidebar ──
async function loadSidebar() {
  var nav = document.getElementById('sidebarNav');
  try {
    var resp = await fetch(BASE + '/family-groups-with-agents');
    var data = await resp.json();
    var groups = data.groups || [];
    // Collect all agents for lookup
    allAgents = [];
    groups.forEach(function(g) { allAgents = allAgents.concat(g.agents); });

    var html = '';

    // "All traces" item
    html += '<div class="nav-all active" onclick="selectAllTraces()" id="navAll">📊 全部 Traces</div>';

    if (groups.length === 0) {
      html += '<div class="loading-tree" style="padding:12px 16px">暂无家庭组</div>';
    } else {
      groups.forEach(function(g, gi) {
        var open = gi === 0 ? ' open' : '';
        html += '<div class="group-item' + open + '">';
        html += '<div class="group-header" onclick="toggleGroup(this)">';
        html += '<span class="group-arrow">▶</span>';
        html += '<span class="group-name" title="' + esc(g.name) + '">🏠 ' + esc(g.name) + '</span>';
        html += '<span class="group-count">' + g.agent_count + '</span>';
        html += '</div>';
        html += '<div class="agent-list">';
        if (g.agents.length === 0) {
          html += '<div class="loading-tree" style="padding:4px 16px 4px 40px;font-size:12px">暂无 Agent</div>';
        } else {
          g.agents.forEach(function(a) {
            var dotClass = a.status === 'online' ? 'online' : a.status === 'offline' ? 'offline' : 'unknown';
            html += '<div class="agent-item" data-agent-id="' + esc(a.id) + '" data-group-id="' + esc(g.id) + '" onclick="selectAgent(\'' + esc(a.id) + '\', \'' + esc(g.id) + '\', this)">';
            html += '<span class="agent-dot ' + dotClass + '"></span>';
            html += '<span class="agent-name" title="' + esc(a.name || a.hostname || a.id) + '">' + esc(a.name || a.hostname || a.id) + '</span>';
            html += '</div>';
          });
        }
        html += '</div></div>';
      });
    }
    nav.innerHTML = html;
  } catch (e) {
    nav.innerHTML = '<div class="loading-tree" style="color:#ef4444">加载失败: ' + esc(e.message) + '</div>';
  }
}

function toggleGroup(header) {
  header.parentElement.classList.toggle('open');
}

function selectAllTraces() {
  currentAgentId = null;
  currentFamilyGroupId = null;
  document.getElementById('contentTitle').textContent = '全部 Traces';
  document.getElementById('contentSubtitle').textContent = '';
  // Update sidebar active states
  document.querySelectorAll('.nav-all,.agent-item').forEach(function(el) { el.classList.remove('active'); });
  document.getElementById('navAll').classList.add('active');
  loadTraces();
}

function selectAgent(agentId, groupId, el) {
  currentAgentId = agentId;
  currentFamilyGroupId = groupId;
  // Find agent name
  var agent = allAgents.find(function(a) { return a.id === agentId; });
  var name = agent ? (agent.name || agent.hostname || agent.id) : agentId;
  document.getElementById('contentTitle').textContent = name;
  document.getElementById('contentSubtitle').textContent = 'Agent: ' + agentId;
  // Update active states
  document.querySelectorAll('.nav-all,.agent-item').forEach(function(el) { el.classList.remove('active'); });
  el.classList.add('active');
  // Expand parent group
  var groupItem = el.closest('.group-item');
  if (groupItem) groupItem.classList.add('open');
  loadTracesByAgent(agentId);
}

// ── Traces loading ──
async function loadTraces() {
  var app = document.getElementById('app');
  app.innerHTML = '<div class="state-msg loading"><div class="spinner"></div><br><small>加载中...</small></div>';
  try {
    var resp = await fetch(BASE + '/traces?limit=50');
    var data = await resp.json();
    if (!data.traces || data.traces.length === 0) {
      app.innerHTML = '<div class="state-msg empty">暂无 trace 数据<br><small>请确保 agent 正在运行并产生 LLM 调用</small></div>';
      return;
    }
    renderTraces(data.traces);
  } catch (e) {
    app.innerHTML = '<div class="state-msg error">加载失败: ' + esc(e.message) + '</div>';
  }
}

async function loadTracesByAgent(agentId) {
  var app = document.getElementById('app');
  app.innerHTML = '<div class="state-msg loading"><div class="spinner"></div><br><small>加载中...</small></div>';
  try {
    var resp = await fetch(BASE + '/traces/by-agent?agent_id=' + encodeURIComponent(agentId) + '&limit=50');
    var data = await resp.json();
    if (!data.traces || data.traces.length === 0) {
      app.innerHTML = '<div class="state-msg empty">该 Agent 暂无 trace 数据<br><small>Agent ID: ' + esc(agentId) + '</small></div>';
      return;
    }
    renderTraces(data.traces);
  } catch (e) {
    app.innerHTML = '<div class="state-msg error">加载失败: ' + esc(e.message) + '</div>';
  }
}

function renderTraces(traces) {
  var app = document.getElementById('app');
  var html = '';
  for (var i = 0; i < traces.length; i++) {
    html += renderTrace(traces[i]);
  }
  app.innerHTML = html;

  // Trace header toggle
  document.querySelectorAll('.trace-header').forEach(function(hdr) {
    hdr.addEventListener('click', function(e) {
      e.stopPropagation();
      hdr.parentElement.classList.toggle('open');
    });
  });

  // Span expand toggle
  document.querySelectorAll('.span-item').forEach(function(item) {
    item.addEventListener('click', function(e) {
      e.stopPropagation();
      item.classList.toggle('expanded');
    });
  });
}

// ── Render helpers ──
function renderMessages(events) {
  if (!Array.isArray(events)) return '';

  var promptEv = events.find(function(e) { return e.name === 'gen_ai.content.prompt'; });
  var completionEv = events.find(function(e) { return e.name === 'gen_ai.content.completion'; });
  var html = '';

  function renderMsgList(raw) {
    var messages;
    try { messages = typeof raw === 'string' ? JSON.parse(raw) : raw; }
    catch(e) { return '<pre>' + esc(String(raw)) + '</pre>'; }
    if (!Array.isArray(messages)) return '<pre>' + esc(String(raw)) + '</pre>';

    var out = '';
    for (var i = 0; i < messages.length; i++) {
      var msg = messages[i];
      var role = msg.role || 'unknown';
      var roleClass = 'role-' + role;
      var content = msg.content || '';
      if (Array.isArray(content)) {
        content = content.map(function(c) { return typeof c === 'object' ? JSON.stringify(c) : String(c); }).join('\n');
      }

      var toolsHtml = '';
      if (msg.tool_calls && Array.isArray(msg.tool_calls)) {
        for (var j = 0; j < msg.tool_calls.length; j++) {
          var tc = msg.tool_calls[j];
          toolsHtml += '<div class="tool-call"><strong>🔧 ' + esc(tc.function ? tc.function.name : 'tool') + '</strong><br><small>' + esc(JSON.stringify(tc.function ? tc.function.arguments : '', null, 2)) + '</small></div>';
        }
      }

      out += '<div class="msg-item"><span class="role ' + roleClass + '">' + role.toUpperCase() + '</span>';
      if (content) out += '<div class="msg-content">' + esc(String(content)) + '</div>';
      out += toolsHtml + '</div>';
    }
    return out;
  }

  if (promptEv && promptEv.attributes && promptEv.attributes['gen_ai.prompt']) {
    html += '<div class="content-block"><h4>📥 Input (Prompt)</h4>' + renderMsgList(promptEv.attributes['gen_ai.prompt']) + '</div>';
  }
  if (completionEv && completionEv.attributes && completionEv.attributes['gen_ai.completion']) {
    html += '<div class="content-block"><h4>📤 Output (Completion)</h4>' + renderMsgList(completionEv.attributes['gen_ai.completion']) + '</div>';
  }
  return html;
}

function renderTrace(trace) {
  var spans = trace.spans || [];
  var firstSpan = spans[0] || {};
  var attrs = firstSpan.attributes || {};
  var serviceType = attrs['langtrace.service.type'] || '';
  var serviceName = attrs['langtrace.service.name'] || '';
  var badgeClass = serviceType === 'llm' ? 'badge-llm' : serviceType === 'framework' ? 'badge-framework' : serviceType === 'vectordb' ? 'badge-vectordb' : 'badge-other';

  var spansHtml = spans.map(function(s) {
    var a = s.attributes || {};
    var inputTokens = a['gen_ai.usage.input_tokens'] || '';
    var outputTokens = a['gen_ai.usage.output_tokens'] || '';
    var model = a['gen_ai.request.model'] || a['gen_ai.response.model'] || '';
    var duration = s.duration ? (s.duration / 1000).toFixed(2) + 's' : '';
    var events = s.events || [];
    var hasContent = Array.isArray(events) && events.length > 0;
    var tagKeys = ['gen_ai.operation.name', 'gen_ai.system', 'langtrace.service.name', 'url.path'];
    var tags = tagKeys.filter(function(k) { return a[k]; }).map(function(k) { return '<span class="attr-tag">' + esc(k.split('.').pop()) + ': ' + esc(a[k]) + '</span>'; }).join('');

    return '<div class="span-item">'
      + '<div class="span-row">'
      + '<span class="span-name">' + esc(s.name) + '</span>'
      + '<span class="span-duration">' + duration + '</span>'
      + '<span class="span-tokens">' + (inputTokens ? 'in:'+inputTokens : '') + ' ' + (outputTokens ? 'out:'+outputTokens : '') + '</span>'
      + '<span class="span-model">' + esc(String(model)) + '</span>'
      + '<span class="span-time">' + (s.start_time || '') + '</span>'
      + '<span class="span-attrs">' + tags + '</span>'
      + (hasContent ? '<span class="expand-hint">▶ 点击查看内容</span>' : '')
      + '</div>'
      + (hasContent ? '<div class="span-detail">' + renderMessages(events) + '</div>' : '')
      + '</div>';
  }).join('');

  return '<div class="trace-card open">'
    + '<div class="trace-header">'
    + '<div class="left">'
    + '<span class="badge ' + badgeClass + '">' + (serviceType || 'span') + '</span>'
    + '<span class="trace-id">' + esc(trace.trace_id) + '</span>'
    + '<span class="span-count">' + spans.length + ' span' + (spans.length > 1 ? 's' : '') + '</span>'
    + (serviceName ? '<span style="font-size:12px;color:#475569">' + esc(serviceName) + '</span>' : '')
    + '</div>'
    + '<div class="right">' + (trace.earliest || '') + '</div>'
    + '</div>'
    + '<div class="span-list">' + spansHtml + '</div>'
    + '</div>';
}

function esc(s) {
  var el = document.createElement('span');
  el.textContent = String(s);
  return el.innerHTML;
}

// ── Init ──
loadSidebar();
loadTraces();
</script>
</body>
</html>"""


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
    print(f"Trace viewer      → http://localhost:{PORT}/traces")
    print(f"API proxy         → {API_BACKEND}")
    print("Ctrl+C to stop")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nstopped.")
