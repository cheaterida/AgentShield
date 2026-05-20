# Stream 2 并行执行 — 主协调文件

> **目标**: 将 Stream 2 剩余 5 个任务（2.3 至 2.8）拆分到 3 个 Claude 终端中并行完成。
> **前置**: Task 2.1（tid/uid）、2.2（execve argv）、2.6（魔数验证）已完成。
> **更新**: 2026-05-20

---

## 任务分配

```
终端 A: stream-2a-sockaddr.md    → Task 2.3 (connect/bind sockaddr)
终端 B: stream-2b-supervisor-build.md → Task 2.5 (supervisor) + 2.4 (build-linux.sh)
终端 C: stream-2c-tests-cleanup.md    → Task 2.7 (测试) + 2.8 (清理)
```

## 启动命令

### 终端 A — connect/bind sockaddr 捕获

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-2a-sockaddr.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行编译验证。
eBPF 编译使用 Docker（镜像 rust:1.91-bookworm），挂载 cargo 缓存 volume 加速：
  docker run --rm -v "C:\Users\Acer\Desktop\AgentShield:/build" -v "agent-shield-cargo:/usr/local/cargo" -w /build rust:1.91-bookworm sh -c "cargo +nightly build -p agentshield-ebpf --target bpfel-unknown-none --release"
```

### 终端 B — Supervisor + build-linux.sh

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-2b-supervisor-build.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行编译验证。
编译使用 Docker（镜像 rust:1.91-bookworm）:
  - agent-runtime: cargo build -p agent-runtime --target x86_64-unknown-linux-musl --release
  - build-linux.sh: bash agent-runtime/build-linux.sh
```

### 终端 C — 集成测试 + 清理

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-2c-tests-cleanup.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行测试验证。
测试执行使用 Docker（镜像 rust:1.91-bookworm）:
  docker run --rm -v "C:\Users\Acer\Desktop\AgentShield:/build" -v "agent-shield-cargo:/usr/local/cargo" -w /build rust:1.91-bookworm sh -c "apt-get install -y -qq musl-tools && cargo test -p agent-runtime"
```

---

## 文件所有权矩阵（零冲突保证）

| 文件 | 终端 A | 终端 B | 终端 C |
|------|--------|--------|--------|
| `ebpf-probes/agentshield-ebpf/src/main.rs` | **修改** | 只读 | 只读 |
| `agent-runtime/src/supervisor.rs` | 只读 | **修改** | 只读 |
| `agent-runtime/build-linux.sh` | 只读 | **修改** | 只读 |
| `agent-runtime/tests/*.rs` | 不碰 | 不碰 | **新建** |
| `agent-runtime/Cargo.toml` | 不碰 | 不碰 | **修改** |
| `ebpf-probes/agentshield-loader/src/main.rs` | 不碰 | 不碰 | **修改** |

**结论：文件集完全互不相交，零 git merge 冲突风险。**

---

## Cargo 缓存加速（CRITICAL — 所有终端必读）

### 问题

Docker 编译每次全新容器启动，无缓存时需要：
- 10 min: apt-get install llvm-dev
- 5 min: rustup install nightly + rust-src
- 5 min: cargo install bpf-linker（从源码编译 30+ crate）
- 2 min: cargo build 下载依赖
**总计：~22 分钟首次编译**

### 解决方案：共享 Docker Volume

第一次编译成功后，所有工具链和依赖缓存于 Docker volume `agent-shield-cargo` 中。后续编译只需 10 秒（仅重新编译变更的代码）。

### 使用方法

所有 Docker 编译命令必须加上 volume 挂载：

```powershell
docker run --rm `
  -v "C:\Users\Acer\Desktop\AgentShield:/build" `
  -v "agent-shield-cargo:/usr/local/cargo" `
  -w /build `
  rust:1.91-bookworm sh -c "..."
```

### 当前缓存状态

volume `agent-shield-cargo` 已包含：
- ✅ nightly-2026-05-20 工具链 + rust-src
- ✅ bpf-linker v0.10.3
- ✅ aya-ebpf 0.1.1 及其依赖
- ✅ agentshield-ebpf-common 编译产物
- ✅ LLVM 14 dev 包（容器内安装，不通过 volume 保存，需每次安装）

**LLVM 仍需每次安装**（不在 volume 中，通过 `apt-get install -y -qq llvm-dev`），但耗时仅 ~30 秒。

### 各终端的编译命令

**终端 A**（eBPF 编译）:
```powershell
docker run --rm -v "C:\Users\Acer\Desktop\AgentShield:/build" -v "agent-shield-cargo:/usr/local/cargo" -w /build rust:1.91-bookworm sh -c "
    apt-get update -qq && apt-get install -y -qq llvm-dev 2>&1 | tail -1
    rustup component add rust-src --toolchain nightly 2>&1 | tail -1
    cargo +nightly build -p agentshield-ebpf --target bpfel-unknown-none --release 2>&1
    ls -lh /build/target/bpfel-unknown-none/release/agentshield-ebpf
"
```

**终端 B**（agent-runtime 编译）:
```powershell
docker run --rm -v "C:\Users\Acer\Desktop\AgentShield:/build" -v "agent-shield-cargo:/usr/local/cargo" -w /build rust:1.91-bookworm sh -c "
    apt-get update -qq && apt-get install -y -qq musl-tools 2>&1 | tail -1
    rustup target add x86_64-unknown-linux-musl 2>&1 | tail -1
    cargo build -p agent-runtime --target x86_64-unknown-linux-musl --release 2>&1
    ls -lh /build/target/x86_64-unknown-linux-musl/release/agent-runtime
"
```

**终端 C**（测试执行）:
```powershell
docker run --rm -v "C:\Users\Acer\Desktop\AgentShield:/build" -v "agent-shield-cargo:/usr/local/cargo" -w /build rust:1.91-bookworm sh -c "
    apt-get update -qq && apt-get install -y -qq musl-tools 2>&1 | tail -1
    cargo test -p agent-runtime 2>&1
"
```

### 注意事项

- **首次编译**：如果 volume 不存在或为空，第一个终端需要完整安装所有工具链（~20 min），后续终端直接受益
- **并发编译**：cargo 自带文件锁，多个 Docker 容器同时 `cargo build` 同一 target 目录时会自动排队，不会冲突
- **清理缓存**：如需重置，`docker volume rm agent-shield-cargo`

---

## 跨终端通信

### 发现问题时

在对应任务计划书文件末尾追加 `## 跨流发现` 节，commit message 使用 `CROSS-STREAM:` 前缀。

### 紧急协调

如果终端 A 的代码改动影响终端 B/C 的理解：
1. 先 commit 当前改动（含完整的 commit message）
2. 在 commit message 中说明影响范围
3. 其他终端 `git pull` 后继续

---

## 合并顺序（全部完成后）

```
1. 终端 B (supervisor + build) — 先合，改动少
2. 终端 A (sockaddr)            — 次合，main.rs 仅改 connect/bind handler
3. 终端 C (tests + cleanup)     — 最后合，测试依赖前两者
```

实际合并顺序无所谓，因为文件集完全不相交。

---

## 协调人验证清单（全部完成后执行）

```bash
# 1. eBPF 字节码
ls -lh target/bpfel-unknown-none/release/agentshield-ebpf
strings target/bpfel-unknown-none/release/agentshield-ebpf | grep agentshield_sys_enter
# 预期: 4 个 tracepoint 符号 + 8KB+ 字节码

# 2. Rust 编译
cargo check --workspace     # 零错误

# 3. Rust lint
cargo clippy --workspace    # 零新警告

# 4. 测试
cargo test -p agent-runtime # 全部通过

# 5. build-linux.sh
bash agent-runtime/build-linux.sh  # 成功生成 bin/agent-runtime (static-pie)

# 6. Docker 内验证
file bin/agent-runtime     # ELF 64-bit ... static-pie
```
