"""Tests for ADFA-LD dataset loading."""

import tempfile
from pathlib import Path

import pytest

from agentshield_ml.data.datasets import (
    SYSCALL_TABLE,
    _parse_trace_file,
    syscall_id_to_name,
)


def test_syscall_table_coverage():
    """Verify common syscalls are mapped."""
    assert SYSCALL_TABLE[0] == "read"
    assert SYSCALL_TABLE[1] == "write"
    assert SYSCALL_TABLE[2] == "open"
    assert SYSCALL_TABLE[59] == "execve"
    assert SYSCALL_TABLE[257] == "openat"
    assert 42 in SYSCALL_TABLE  # connect


def test_syscall_id_to_name_known():
    assert syscall_id_to_name(59) == "execve"
    assert syscall_id_to_name(257) == "openat"


def test_syscall_id_to_name_unknown():
    name = syscall_id_to_name(9999)
    assert name.startswith("syscall_")


def test_parse_trace_file():
    """Parse a synthetic ADFA-LD trace file."""
    content = "308 59\n308 257\n308 2\n308 0\n"
    with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False) as f:
        f.write(content)
        tmp_path = f.name

    try:
        entries = _parse_trace_file(tmp_path)
        assert len(entries) == 4
        assert entries[0] == (308, 59)   # pid=308, execve
        assert entries[1] == (308, 257)  # pid=308, openat
        assert entries[2] == (308, 2)    # pid=308, open
        assert entries[3] == (308, 0)    # pid=308, read
    finally:
        Path(tmp_path).unlink(missing_ok=True)


def test_parse_trace_file_empty_lines():
    """Empty and whitespace-only lines are ignored."""
    content = "308 59\n\n  \n308 0\n"
    with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False) as f:
        f.write(content)
        tmp_path = f.name

    try:
        entries = _parse_trace_file(tmp_path)
        assert len(entries) == 2
    finally:
        Path(tmp_path).unlink(missing_ok=True)


def test_parse_trace_file_invalid_line():
    """Lines with invalid numbers are skipped."""
    content = "308 59\ninvalid line\n308 0\n"
    with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False) as f:
        f.write(content)
        tmp_path = f.name

    try:
        entries = _parse_trace_file(tmp_path)
        assert len(entries) == 2
    finally:
        Path(tmp_path).unlink(missing_ok=True)
