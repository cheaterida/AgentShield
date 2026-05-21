#!/usr/bin/env bash
# ── Track A 快速测试脚本 ──
# 在 Docker 容器中编译并测试 agent-runtime (含 checkpoint feature)
# 前提: Docker Desktop 运行中
# 首次运行会拉取 rust:1.91-bookworm（~1GB），之后使用缓存加速
#
# 国内加速：Docker Desktop → Settings → Docker Engine → 添加:
#   "registry-mirrors": ["https://docker.m.daocloud.io"]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Windows 路径转换（Git Bash / MSYS2）
WIN_ROOT="$(cd "$REPO_ROOT" && pwd -W 2>/dev/null || echo "$REPO_ROOT")"

echo "============================================"
echo "  Track A — agent-runtime build + test"
echo "  feature: checkpoint"
echo "============================================"

docker run --rm \
  -v "${WIN_ROOT}:/build" \
  -v "agent-shield-cargo:/usr/local/cargo" \
  -e CARGO_REGISTRIES_CRATES_IO_PROTOCOL=sparse \
  -e RUSTUP_DIST_SERVER=https://mirrors.ustc.edu.cn/rust-static \
  -e RUSTUP_UPDATE_ROOT=https://mirrors.ustc.edu.cn/rust-static/rustup \
  --workdir //build \
  rust:1.91-bookworm \
  sh -c '
set -e

# ── 配置国内镜像 ──
mkdir -p /usr/local/cargo
cat > /usr/local/cargo/config.toml << "EOF"
[source.crates-io]
replace-with = "ustc"
[source.ustc]
registry = "sparse+https://mirrors.ustc.edu.cn/crates.io-index/"
EOF

echo ">>> Cargo mirror: USTC"

# ── 安装构建依赖 ──
apt-get update -qq 2>&1 | tail -1
apt-get install -y -qq musl-tools 2>&1 | tail -1

# ── Step 1: 编译 (checkpoint feature) ──
echo ""
echo ">>> Building agent-runtime (--features checkpoint)..."
cargo build -p agent-runtime --features checkpoint 2>&1

echo ""
echo ">>> Build OK"

# ── Step 2: 运行单元测试 ──
echo ""
echo ">>> Running unit tests..."
cargo test -p agent-runtime --features checkpoint 2>&1

echo ""
echo ">>> All tests OK"

# ── Step 3: Clippy 检查 ──
echo ""
echo ">>> Running clippy..."
rustup component add clippy 2>&1 | tail -1
cargo clippy -p agent-runtime --features checkpoint -- -D warnings 2>&1

echo ""
echo "============================================"
echo "  All checks passed!"
echo "============================================"
'

echo ""
echo "=== Done ==="
