#!/usr/bin/env bash
# Cross-compile agent-runtime for Linux (static musl binary + eBPF bytecode).
#
# Uses a persistent Docker container (agentshield-build) with all toolchains
# pre-installed. First run builds the image (~5 min), subsequent runs skip that.
#
# Prerequisites: Docker Desktop running.
# Output: agent-runtime/bin/agent-runtime (static-pie x86_64)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# Docker Desktop on Windows needs Windows-style paths for volume mounts
WIN_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -W 2>/dev/null || echo "$REPO_ROOT")"

IMAGE="agentshield/build:latest"
CONTAINER="agentshield-build"
CARGO_CACHE="agent-shield-cargo"

echo "=== AgentShield build environment (persistent container) ==="

# ── 1. Ensure the build image exists ──
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
    echo "--- Building persistent build image (one-time, ~3-5 min) ---"
    docker build -t "$IMAGE" -f "$SCRIPT_DIR/Dockerfile.build" "$REPO_ROOT"
    echo "--- Image ready: $IMAGE ---"
else
    echo "--- Build image already exists: $IMAGE ---"
fi

# ── 2. Ensure the persistent container exists ──
if ! docker container inspect "$CONTAINER" >/dev/null 2>&1; then
    echo "--- Creating persistent build container ---"
    docker run -d --name "$CONTAINER" \
        -v "${WIN_ROOT}://build" \
        -v "${CARGO_CACHE}://usr/local/cargo" \
        "$IMAGE"
    echo "--- Container created: $CONTAINER ---"
else
    # Start if stopped
    if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER"; then
        echo "--- Starting existing container ---"
        docker start "$CONTAINER"
    fi
    echo "--- Container ready: $CONTAINER ---"
fi

# ── 3. Build inside the persistent container ──
echo ""
echo "=== Compiling (eBPF bytecode + agent-runtime) ==="

docker exec -i "$CONTAINER" sh -c '
set -e

# ── eBPF bytecode ──
echo "--- Building eBPF bytecode ---"
cargo +nightly build -p agentshield-ebpf \
    --target bpfel-unknown-none \
    --release \
    -Z build-std=core

ls -lh /build/target/bpfel-unknown-none/release/agentshield-ebpf

# ── agent-runtime (musl static) ──
echo "--- Building agent-runtime (musl static) ---"
cargo build -p agent-runtime \
    --target x86_64-unknown-linux-musl \
    --release

ls -lh /build/target/x86_64-unknown-linux-musl/release/agent-runtime
'

# ── 4. Copy binary out ──
OUT_DIR="$SCRIPT_DIR/bin"
mkdir -p "$OUT_DIR"
cp "$REPO_ROOT/target/x86_64-unknown-linux-musl/release/agent-runtime" "$OUT_DIR/agent-runtime"
chmod +x "$OUT_DIR/agent-runtime" 2>/dev/null || true

echo ""
echo "=== Binary ready: $OUT_DIR/agent-runtime ($(du -h "$OUT_DIR/agent-runtime" | cut -f1)) ==="
file "$OUT_DIR/agent-runtime"
