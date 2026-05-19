"""Tests for syscall trace → audit event conversion."""

from datetime import datetime, timezone

from agentshield_ml.data.preprocess import (
    SyscallTrace,
    syscall_id_to_name,
    syscall_to_audit_events,
    build_cfg_dataset,
)


def test_syscall_trace_creation():
    trace = SyscallTrace(
        trace_id="test-001",
        pid=1234,
        syscalls=["openat", "read", "write", "close"],
        syscall_ids=[257, 0, 1, 3],
        label="normal",
    )
    assert len(trace) == 4
    assert trace.label == "normal"


def test_syscall_to_audit_events_basic():
    trace = SyscallTrace(
        trace_id="t1",
        pid=500,
        syscalls=["openat", "read", "close"],
        syscall_ids=[257, 0, 3],
    )
    events = syscall_to_audit_events(
        trace,
        agent_id="agent-1",
        family_group_id="fg1",
    )
    assert len(events) == 3

    # First event
    assert events[0]["agent_id"] == "agent-1"
    assert events[0]["family_group_id"] == "fg1"
    assert events[0]["action"] == "openat"
    assert events[0]["attributes"]["syscall_name"] == "openat"
    assert events[0]["attributes"]["syscall_id"] == "257"
    assert events[0]["attributes"]["label"] == "normal"

    # Second event
    assert events[1]["action"] == "read"
    assert events[0]["resource_ref"] is not None
    assert len(events[0]["resource_ref"]) > 0


def test_syscall_to_audit_events_event_ids_unique():
    trace = SyscallTrace(
        trace_id="t1",
        pid=100,
        syscalls=["openat", "read", "write"],
        syscall_ids=[257, 0, 1],
    )
    events = syscall_to_audit_events(trace, agent_id="a1")
    ids = [e["event_id"] for e in events]
    assert len(ids) == len(set(ids))  # all unique


def test_syscall_to_audit_events_timestamps_increasing():
    trace = SyscallTrace(
        trace_id="t1",
        pid=100,
        syscalls=["openat", "read", "close"],
        syscall_ids=[257, 0, 3],
    )
    base = datetime(2024, 6, 15, tzinfo=timezone.utc)
    events = syscall_to_audit_events(
        trace, agent_id="a1", base_time=base, time_step_ms=100,
    )
    t0 = events[0]["occurred_at"]
    t1 = events[1]["occurred_at"]
    t2 = events[2]["occurred_at"]
    assert t0 < t1 < t2


def test_build_cfg_dataset():
    """Build a per-agent dataset from synthetic traces."""
    traces = {
        "normal": [
            [(100, 257), (100, 0), (100, 3)],   # openat, read, close
            [(200, 257), (200, 1), (200, 3)],   # openat, write, close
        ],
        "meterpreter": [
            [(300, 59), (300, 257), (300, 0)],  # execve, openat, read
        ],
    }
    result = build_cfg_dataset(traces, max_traces_per_category=10)
    assert len(result) == 3  # 3 agents (2 normal + 1 attack)

    # Normal agent events should have label="normal"
    for agent_id, events in result.items():
        if "normal" in agent_id:
            for ev in events:
                assert ev["attributes"]["label"] == "normal"
        assert len(events) > 0


def test_build_cfg_dataset_empty():
    result = build_cfg_dataset({})
    assert len(result) == 0
