#!/usr/bin/env bash
# Cross-compile agent-runtime for Linux (static musl binary + embedded eBPF bytecode).
# Prerequisites: Docker Desktop running.
# Output: agent-runtime/bin/agent-runtime (static x86_64 binary)
set -euo pipefail
cd "$(dirname "$0")"

echo "=== Building agent-runtime for linux/amd64 (musl + eBPF) ==="

# ── Prepare build context ──────────────────────────────────────────────
BUILD_DIR="$(pwd)/.build-context"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/src"
cp -r src/* "$BUILD_DIR/src/"

# Copy ebpf-common source (ProbeEvent type needed by agent-runtime)
mkdir -p "$BUILD_DIR/ebpf-common/src"
cp ../ebpf-probes/agentshield-ebpf-common/src/lib.rs "$BUILD_DIR/ebpf-common/src/"

# Copy eBPF probe source for bytecode compilation
mkdir -p "$BUILD_DIR/ebpf-probe/src"
cp ../ebpf-probes/agentshield-ebpf/src/main.rs "$BUILD_DIR/ebpf-probe/src/"

# Copy workspace Cargo.toml parts for dependency resolution
cat > "$BUILD_DIR/ebpf-common/Cargo.toml" <<'CMN'
[package]
name = "agentshield-ebpf-common"
version = "0.1.0"
edition = "2021"

[dependencies]
aya-ebpf = "0.1"
CMN

cat > "$BUILD_DIR/ebpf-probe/Cargo.toml" <<'PRB'
[package]
name = "agentshield-ebpf"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["lib"]
path = "src/main.rs"

[dependencies]
aya-ebpf = "0.1"
agentshield-ebpf-common = { path = "../ebpf-common" }
PRB

# Generate standalone Cargo.toml for agent-runtime
cat > "$BUILD_DIR/Cargo.toml" <<'CARGO'
[package]
name = "agent-runtime"
version = "0.3.0"
edition = "2021"

[[bin]]
name = "agent-runtime"
path = "src/main.rs"

[dependencies]
tokio = { version = "1.40", features = ["macros", "rt-multi-thread", "signal", "sync", "time"] }
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
thiserror = "2"
reqwest = { version = "0.12", default-features = false, features = ["json", "rustls-tls"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
sysinfo = "0.31"
aya = { version = "0.13", features = ["async_tokio"] }
agentshield-ebpf-common = { path = "./ebpf-common" }
bytes = "1"
num_cpus = "1"
CARGO

echo "--- Starting Docker build (eBPF bytecode + agent-runtime) ---"
docker run --rm \
  -v "${BUILD_DIR}:/build" \
  -w /build \
  rust:1.87-alpine \
  sh -c '
    set -e

    apk add --no-cache musl-dev linux-headers

    # ── Stage 1: compile eBPF bytecode (nightly rust needed) ──
    echo "=== Stage 1: eBPF bytecode ==="
    rustup install nightly
    rustup component add rust-src --toolchain nightly
    rustup target add bpfel-unknown-none --toolchain nightly
    # Use CARGO_TARGET_DIR so bytecode lands where agent-runtime expects it (../target/bpfel-unknown-none/release/)
    export CARGO_TARGET_DIR=/build/target
    cargo +nightly build -Z build-std=core --target bpfel-unknown-none --release \
      --manifest-path ebpf-probe/Cargo.toml 2>&1 || {
      echo "WARNING: eBPF bytecode compilation failed, agent will use demo mode"
      mkdir -p /build/target/bpfel-unknown-none/release
      touch /build/target/bpfel-unknown-none/release/agentshield-ebpf
    }
    echo "eBPF bytecode: $(ls -la /build/target/bpfel-unknown-none/release/agentshield-ebpf 2>/dev/null || echo 'not found')"

    # ── Stage 2: compile agent-runtime (stable musl) ──
    echo "=== Stage 2: agent-runtime ==="
    rustup target add x86_64-unknown-linux-musl
    cargo build --release --target x86_64-unknown-linux-musl
  '

# Copy binary out
mkdir -p bin
cp "$BUILD_DIR/target/x86_64-unknown-linux-musl/release/agent-runtime" bin/agent-runtime 2>/dev/null || {
  echo "Binary not found in expected location, searching..."
  find "$BUILD_DIR" -name agent-runtime -type f 2>/dev/null
}
chmod +x bin/agent-runtime 2>/dev/null || true

# Cleanup
rm -rf "$BUILD_DIR"

echo ""
if [ -f bin/agent-runtime ]; then
  echo "=== Binary ready: bin/agent-runtime ($(du -h bin/agent-runtime | cut -f1)) ==="
  file bin/agent-runtime
else
  echo "=== Build failed: no binary produced ==="
  exit 1
fi
