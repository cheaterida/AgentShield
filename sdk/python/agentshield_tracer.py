"""
AgentShield Lightweight Tracer — replaces langtrace-python-sdk.

Usage:
    from agentshield_tracer import init_tracer, trace_llm_call

    init_tracer(
        agent_id="hermes-vm-agent-001",
        family_group_id="default",
        management_url="http://100.123.70.98:8081",
    )

    # Manual tracing
    with trace_llm_call(model="deepseek-v4-pro", system="deepseek") as span:
        # ... make your LLM call ...
        span.set_prompt([{"role": "user", "content": "Hello"}])
        span.set_completion([{"role": "assistant", "content": "Hi!"}])
        span.set_usage(input_tokens=10, output_tokens=20)

    # Auto-wrap OpenAI client
    from agentshield_tracer import wrap_openai
    client = wrap_openai(openai.OpenAI())
    # All calls are now automatically traced

Sends spans to: POST {management_url}/api/v1/spans
"""

import json
import os
import time
import uuid
import base64
import threading
from typing import Optional

_AGENT_ID = ""
_FAMILY_GROUP_ID = ""
_MGMT_URL = "http://localhost:8080"
_HTTP_SESSION = None


def _get_session():
    global _HTTP_SESSION
    if _HTTP_SESSION is None:
        import requests
        _HTTP_SESSION = requests.Session()
    return _HTTP_SESSION


def init_tracer(
    agent_id: str = "",
    family_group_id: str = "",
    management_url: str = "http://localhost:8080",
):
    """Initialize the AgentShield tracer. Call once at startup."""
    global _AGENT_ID, _FAMILY_GROUP_ID, _MGMT_URL
    _AGENT_ID = agent_id or os.environ.get("AGENTSHIELD_AGENT_ID", "")
    _FAMILY_GROUP_ID = family_group_id or os.environ.get("AGENTSHIELD_FAMILY_GROUP_ID", "")
    _MGMT_URL = management_url or os.environ.get("AGENTSHIELD_MGMT_ADDR", "http://localhost:8080")


def _make_id() -> str:
    raw = uuid.uuid4().bytes + uuid.uuid4().bytes
    return base64.b64encode(raw).decode("ascii").rstrip("=")


def _flush_spans(spans: list[dict]):
    if not spans:
        return
    try:
        sess = _get_session()
        sess.post(
            f"{_MGMT_URL}/api/v1/spans",
            json={"spans": spans},
            headers={
                "Content-Type": "application/json",
                "X-AgentShield-Agent-ID": _AGENT_ID,
                "X-AgentShield-Family-Group-ID": _FAMILY_GROUP_ID,
            },
            timeout=15,
        )
    except Exception:
        pass


def _now_ns() -> int:
    return int(time.time() * 1_000_000_000)


def _ns_to_str(ns: int) -> str:
    import datetime as _dt
    return _dt.datetime.fromtimestamp(ns / 1e9, tz=_dt.timezone.utc).strftime(
        "%Y-%m-%d %H:%M:%S."
    ) + f"{ns % 1_000_000_000 // 1_000:03d}"


class Span:
    """A single span representing an LLM call or operation."""

    def __init__(self, name: str, kind: int = 1, trace_id: str = "", parent_id: str = ""):
        self.trace_id = trace_id or _make_id()
        self.span_id = _make_id()
        self.parent_id = parent_id
        self.name = name
        self.kind = kind  # 1 = INTERNAL, 3 = CLIENT, etc.
        self.start_ns = _now_ns()
        self.end_ns = 0
        self.attrs = {}
        self.events = []
        self._ended = False

    def set_attribute(self, key: str, value: str):
        self.attrs[key] = value

    def set_attributes(self, attrs: dict):
        self.attrs.update(attrs)

    def add_event(self, name: str, attrs: dict = None):
        self.events.append({
            "name": name,
            "timeUnixNano": _now_ns(),
            "attributes": attrs or {},
        })

    def set_prompt(self, messages: list[dict]):
        self.add_event("gen_ai.content.prompt", {"gen_ai.prompt": json.dumps(messages)})

    def set_completion(self, messages: list[dict]):
        self.add_event("gen_ai.content.completion", {"gen_ai.completion": json.dumps(messages)})

    def set_usage(self, input_tokens: int = 0, output_tokens: int = 0, total_tokens: int = 0):
        self.attrs["gen_ai.usage.input_tokens"] = str(input_tokens)
        self.attrs["gen_ai.usage.output_tokens"] = str(output_tokens)
        self.attrs["gen_ai.usage.total_tokens"] = str(total_tokens or input_tokens + output_tokens)

    def end(self):
        if self._ended:
            return
        self._ended = True
        self.end_ns = _now_ns()

    def to_dict(self) -> dict:
        end_ns = self.end_ns or _now_ns()
        return {
            "trace_id": self.trace_id,
            "span_id": self.span_id,
            "parent_id": self.parent_id,
            "name": self.name,
            "kind": self.kind,
            "start_time": _ns_to_str(self.start_ns),
            "end_time": _ns_to_str(end_ns),
            "duration": (end_ns - self.start_ns) // 1_000_000,
            "attributes": self.attrs,
            "events": self.events,
            "agent_id": _AGENT_ID,
            "family_group_id": _FAMILY_GROUP_ID,
            "project_name": "agentshield",
        }


class trace_llm_call:
    """Context manager for tracing an LLM call.

    with trace_llm_call(model="deepseek-v4-pro", system="deepseek") as span:
        response = client.chat.completions.create(...)
        span.set_prompt(messages)
        span.set_completion([{"role": "assistant", "content": response.choices[0].message.content}])
    """

    def __init__(self, model: str = "", system: str = "", operation: str = "chat"):
        self.model = model
        self.system = system
        self.operation = operation
        self.span: Optional[Span] = None

    def __enter__(self) -> Span:
        name = f"{self.system}.chat.completions.create" if self.system else "llm.chat.completions.create"
        self.span = Span(name=name, kind=3)
        self.span.set_attributes({
            "gen_ai.operation.name": self.operation,
            "gen_ai.system": self.system,
            "agentshield.service.name": self.system or "LLM",
            "agentshield.service.type": "llm",
            "langtrace.service.name": self.system or "LLM",
            "langtrace.service.type": "llm",
        })
        if self.model:
            self.span.set_attribute("gen_ai.request.model", self.model)
        return self.span

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.span:
            self.span.end()
            _flush_spans([self.span.to_dict()])
        return False


def wrap_openai(client, system: str = "openai"):
    """Wrap an OpenAI client to automatically trace all calls.

    from agentshield_tracer import init_tracer, wrap_openai
    import openai

    init_tracer(agent_id="agent-001", family_group_id="default")
    client = wrap_openai(openai.OpenAI())
    # All chat.completions.create calls are now auto-traced
    """
    original_create = client.chat.completions.create

    def traced_create(*args, **kwargs):
        model = kwargs.get("model", "unknown")
        messages = kwargs.get("messages", [])

        span = Span(
            name=f"{system}.chat.completions.create",
            kind=3,
        )
        span.set_attributes({
            "gen_ai.operation.name": "chat",
            "gen_ai.system": system,
            "gen_ai.request.model": model,
            "agentshield.service.name": system or "LLM",
            "agentshield.service.type": "llm",
            "langtrace.service.name": system or "LLM",
            "langtrace.service.type": "llm",
        })

        # Record prompt
        if messages:
            span.set_prompt([
                {"role": m.get("role", "unknown"), "content": m.get("content", "")}
                for m in messages
            ])

        try:
            response = original_create(*args, **kwargs)

            # Record completion
            usage = getattr(response, "usage", None)
            if usage:
                span.set_usage(
                    input_tokens=getattr(usage, "prompt_tokens", 0),
                    output_tokens=getattr(usage, "completion_tokens", 0),
                    total_tokens=getattr(usage, "total_tokens", 0),
                )
            span.set_attribute("gen_ai.response.model", getattr(response, "model", model))

            choices = getattr(response, "choices", [])
            if choices:
                msg = getattr(choices[0], "message", None)
                if msg:
                    span.set_completion([
                        {"role": getattr(msg, "role", "assistant"),
                         "content": getattr(msg, "content", "")}
                    ])

            span.end()
            _flush_spans([span.to_dict()])
            return response
        except Exception as e:
            span.set_attribute("error", str(e))
            span.end()
            _flush_spans([span.to_dict()])
            raise

    client.chat.completions.create = traced_create
    return client


# ── Auto-init from env ──
if os.environ.get("AGENTSHIELD_TRACER_AUTO_INIT") == "1":
    init_tracer()
