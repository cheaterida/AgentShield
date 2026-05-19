#!/usr/bin/env bash
# AgentShield build script — compiles everything and packages for distribution.
# Usage: ./build.sh

set -euo pipefail
cd "$(dirname "$0")"

PROFILE="${1:-release}"
RELEASE_FLAG=""
if [ "$PROFILE" = "release" ]; then RELEASE_FLAG="--release"; fi
PROFILE_DIR="$PROFILE"

echo "=== 1/3: Build eBPF probe (nightly) ==="
cargo +nightly build $RELEASE_FLAG --target bpfel-unknown-none -p agentshield-ebpf

echo "=== 2/3: Build agent-runtime (musl static) ==="
cargo build $RELEASE_FLAG --target x86_64-unknown-linux-musl -p agent-runtime

echo "=== 3/3: Package distribution ==="
rm -rf dist/bin dist/scripts
mkdir -p dist/bin dist/scripts

# agent-runtime static binary
cp "target/x86_64-unknown-linux-musl/$PROFILE_DIR/agent-runtime" dist/bin/agent-runtime

# eBPF bytecode
EBPF_BIN=$(find "target/bpfel-unknown-none/$PROFILE_DIR/deps" \
    -name 'agentshield_ebpf-*' \
    -not -name '*.d' -not -name '*.o' -not -name '*.rcgu.o' \
    -type f 2>/dev/null | head -1)
if [ -z "$EBPF_BIN" ]; then
    echo "ERROR: eBPF binary not found"
    exit 1
fi
cp "$EBPF_BIN" dist/bin/agentshield-ebpf.o

# checksums
sha256sum dist/bin/agent-runtime dist/bin/agentshield-ebpf.o > dist/bin/agent.sha256

echo ""
echo "=== Build complete ==="
echo "dist/bin/agent-runtime     ($(du -h dist/bin/agent-runtime | cut -f1))"
echo "dist/bin/agentshield-ebpf.o ($(du -h dist/bin/agentshield-ebpf.o | cut -f1))"
echo ""
echo "启动分发服务器:  ./dist/serve.sh"
echo "（员工机器用 Tailscale IP 或 hostname 拉取）"
