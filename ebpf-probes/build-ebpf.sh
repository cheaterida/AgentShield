#!/bin/bash
# Build eBPF bytecode from agentshield-ebpf probe.
# Requires: Docker (or Linux host with bpf-linker + nightly rustc).
# Output: target/bpfel-unknown-none/release/agentshield-ebpf
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "=== Installing bpf-linker ==="
cargo +nightly install bpf-linker 2>/dev/null || true

echo "=== Building eBPF bytecode ==="
cargo +nightly build -p agentshield-ebpf \
  --target bpfel-unknown-none \
  --release \
  -Z build-std=core

BYTECODE="target/bpfel-unknown-none/release/agentshield-ebpf"
if [ -f "$BYTECODE" ]; then
    echo "=== eBPF bytecode built: $BYTECODE ($(du -h "$BYTECODE" | cut -f1)) ==="
else
    echo "=== ERROR: bytecode not found at $BYTECODE ==="
    exit 1
fi
