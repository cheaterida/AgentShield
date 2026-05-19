"""Open-source dataset loaders for agent anomaly detection training.

Supported datasets:
  - ADFA-LD: system-call traces from normal + 6 attack scenarios
  - LID-DS (planned): host-based intrusion detection with sysdig/auditd events
"""

from .datasets import load_adfa_ld, list_adfa_traces
from .preprocess import syscall_to_audit_events, SyscallTrace, build_cfg_dataset

__all__ = [
    "load_adfa_ld",
    "list_adfa_traces",
    "syscall_to_audit_events",
    "SyscallTrace",
    "build_cfg_dataset",
]
