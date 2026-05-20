#!/usr/bin/env bash
# Cross-compile agent-runtime for Linux (static musl binary).
# Prerequisites: Docker Desktop running.
# Output: agent-runtime/bin/agent-runtime (static x86_64 binary)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Docker Desktop on Windows needs Windows-style paths
WIN_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -W)"

echo "=== Building agent-runtime for linux/amd64 (musl) ==="

docker run --rm \
  -v "${WIN_ROOT}:/build" \
  -v "agent-shield-cargo:/usr/local/cargo" \
  --workdir //build \
  rust:1.91-bookworm \
  sh -c '
    set -e

    apt-get update -qq && apt-get install -y -qq musl-tools llvm-dev 2>&1 | tail -1

    # ── Build eBPF bytecode ──
    echo "=== Building eBPF bytecode ==="
    rustup component add rust-src --toolchain nightly 2>&1 | tail -1
    cargo +nightly install bpf-linker 2>&1 | tail -1
    cargo +nightly build -p agentshield-ebpf \
      --target bpfel-unknown-none \
      --release \
      -Z build-std=core 2>&1

    ls -lh /build/target/bpfel-unknown-none/release/agentshield-ebpf

    # ── Build agent-runtime ──
    echo "=== Building agent-runtime ==="
    rustup target add x86_64-unknown-linux-musl 2>&1 | tail -1
    cargo build -p agent-runtime \
      --target x86_64-unknown-linux-musl \
      --release 2>&1

    ls -lh /build/target/x86_64-unknown-linux-musl/release/agent-runtime
  '

# ── Copy binary to output path ──
OUT_DIR="$SCRIPT_DIR/bin"
mkdir -p "$OUT_DIR"
cp "$REPO_ROOT/target/x86_64-unknown-linux-musl/release/agent-runtime" "$OUT_DIR/agent-runtime"
chmod +x "$OUT_DIR/agent-runtime" 2>/dev/null || true

echo ""
echo "=== Binary ready: $OUT_DIR/agent-runtime ($(du -h "$OUT_DIR/agent-runtime" | cut -f1)) ==="
file "$OUT_DIR/agent-runtime"
