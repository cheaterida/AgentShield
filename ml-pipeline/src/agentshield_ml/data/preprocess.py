"""System-call trace → AuditEvent conversion for ML training.

Converts raw ADFA-LD traces (pid, syscall_id) into the AuditEvent dict
format expected by the CAE encoder / CFG builder.
"""

from __future__ import annotations

import hashlib
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from typing import Optional

from .datasets import SYSCALL_TABLE


# Heuristic mapping: which syscalls imply which resource_ref.
# Used to generate plausible resource_ref strings for training data
# where actual paths / addresses are not recorded.
SYSCALL_RESOURCE_HINTS: dict[str, str] = {
    "open": "/data/files/document.txt",
    "openat": "/data/files/document.txt",
    "read": "/data/files/input.bin",
    "write": "/data/output/results.json",
    "execve": "/usr/bin/python3",
    "execveat": "/usr/bin/python3",
    "connect": "10.0.0.1:443",
    "bind": "0.0.0.0:8080",
    "accept": "0.0.0.0:8080",
    "stat": "/etc/config.toml",
    "newfstatat": "/etc/config.toml",
    "lstat": "/etc/config.toml",
    "access": "/etc/passwd",
    "faccessat": "/etc/passwd",
    "chmod": "/data/files/script.sh",
    "chown": "/data/files/script.sh",
    "unlink": "/tmp/cache/temp.dat",
    "mkdir": "/data/newdir",
    "rename": "/data/files/oldname.txt",
    "socket": "AF_INET",
    "sendto": "10.0.0.1:443",
    "recvfrom": "10.0.0.1:443",
    "mmap": "/lib/x86_64-linux-gnu/libc.so.6",
    "mount": "/mnt/data",
    "clone": "/usr/bin/bash",
    "fork": "/usr/bin/bash",
    "kill": "signal:SIGTERM",
    "ptrace": "/proc/self/mem",
    "capset": "CAP_SYS_ADMIN",
    "init_module": "/lib/modules/kernel.ko",
    "delete_module": "module:example",
    "bpf": "BPF_PROG_LOAD",
    "prctl": "PR_SET_SECCOMP",
    "setuid": "0",
    "setgid": "0",
}


@dataclass
class SyscallTrace:
    """A parsed system-call trace ready for conversion."""
    trace_id: str
    pid: int
    syscalls: list[str]  # sequential syscall names
    syscall_ids: list[int] = field(default_factory=list)
    label: str = "normal"  # "normal" or attack name

    def __len__(self) -> int:
        return len(self.syscalls)


def syscall_id_to_name(sc_id: int) -> str:
    """Convert Linux x86_64 syscall number to name."""
    return SYSCALL_TABLE.get(sc_id, f"syscall_{sc_id}")


def syscall_to_audit_events(
    trace: SyscallTrace,
    agent_id: str = "",
    family_group_id: str = "default",
    base_time: Optional[datetime] = None,
    time_step_ms: int = 50,
) -> list[dict]:
    """Convert a SyscallTrace into a list of AuditEvent dicts.

    Each event includes:
      - event_id, occurred_at, family_group_id, agent_id
      - action (syscall name)
      - resource_ref (heuristic path / address)
      - attributes (pid, syscall_id, label, etc.)

    Timestamps are interpolated: *base_time* + index × *time_step_ms*.
    """
    if base_time is None:
        base_time = datetime(2024, 1, 1, tzinfo=timezone.utc)

    events: list[dict] = []
    for i, sc_name in enumerate(trace.syscalls):
        sc_id = trace.syscall_ids[i] if i < len(trace.syscall_ids) else 0
        resource_ref = SYSCALL_RESOURCE_HINTS.get(sc_name, f"syscall:{sc_name}")
        occurred_at = (base_time + timedelta(milliseconds=i * time_step_ms)).isoformat()

        event = {
            "event_id": hashlib.sha256(
                f"{trace.trace_id}:{i}:{sc_name}".encode()
            ).hexdigest()[:16],
            "occurred_at": occurred_at,
            "family_group_id": family_group_id,
            "agent_id": agent_id or f"trace-{trace.pid}",
            "action": sc_name,
            "resource_ref": resource_ref,
            "attributes": {
                "pid": str(trace.pid),
                "syscall_id": str(sc_id),
                "syscall_name": sc_name,
                "trace_id": trace.trace_id,
                "label": trace.label,
                "seq": str(i),
            },
        }
        events.append(event)
    return events


def build_cfg_dataset(
    traces: dict[str, list[list[tuple[int, int]]]],
    *,
    agent_id_prefix: str = "adfa-agent",
    family_group_id: str = "default",
    max_traces_per_category: int = 100,
) -> dict[str, list[dict]]:
    """Convert raw ADFA-LD traces into per-agent audit event lists.

    Returns ``{agent_id: [AuditEvent, ...]}`` suitable for
    ``CFGBuilder.build_from_agent_groups()``.
    """
    result: dict[str, list[dict]] = {}
    agent_idx = 0

    for category, trace_list in traces.items():
        for ti, trace_data in enumerate(trace_list[:max_traces_per_category]):
            agent_id = f"{agent_id_prefix}-{category}-{ti:04d}"
            syscall_names = [syscall_id_to_name(sc) for _, sc in trace_data]
            syscall_ids = [sc for _, sc in trace_data]
            pid = trace_data[0][0] if trace_data else 0

            st = SyscallTrace(
                trace_id=f"{category}-{ti:04d}",
                pid=pid,
                syscalls=syscall_names,
                syscall_ids=syscall_ids,
                label=category,
            )
            events = syscall_to_audit_events(st, agent_id=agent_id, family_group_id=family_group_id)
            if events:
                result[agent_id] = events
            agent_idx += 1

    return result
