# Stream 2: Rust / eBPF 数据捕获与构建修复

> **终端标识**: `stream-2-rust-ebpf`
> **组件**: `agent-runtime/` + `ebpf-probes/` (Rust workspace, Tokio + Aya)
> **依赖**: 无外部流依赖，可立即开工
> **预计工时**: 6-8h
> **注意**: eBPF 编译需 Linux 环境（Docker）。非 Linux 环境下可先完成 Rust 侧修复。

---

## 任务清单

### Task 2.1 — eBPF 探针捕获 tid 和 uid 🔴 Critical

**文件**: [main.rs](ebpf-probes/agentshield-ebpf/src/main.rs:26-135)

**问题**: 所有 4 个 tracepoint handler 中 `tid` 和 `uid` 硬编码为 `0`。BPF helper 可用但未调用。

**修复**:
1. 在每个 handler 中调用 `bpf_get_current_pid_tgid()` → 低 32 位为 `tid`（内核态为用户态 TID），高 32 位为 `tgid`（= 用户态 PID）
2. 调用 `bpf_get_current_uid_gid()` → 低 32 位为 `uid`
3. 更新 `ProbeEvent` 的 `pid` 字段为 `tgid`（`(pid_tgid >> 32) as u32`），`tid` 为 `(pid_tgid & 0xFFFF_FFFF) as u32`

**具体改动**（每个 handler 内）:
```rust
let pid_tgid = bpf_get_current_pid_tgid();
let uid_gid = bpf_get_current_uid_gid();
event.pid = (pid_tgid >> 32) as u32;
event.tid = pid_tgid as u32;
event.uid = uid_gid as u32;
```

### Task 2.2 — eBPF execve 探针捕获 argv 🔴 Critical

**文件**: [main.rs](ebpf-probes/agentshield-ebpf/src/main.rs:58-87)

**问题**: `execve` handler 只读取 `filename`（`ctx.read_at(16)`，即 `const char *filename`），`argv` 始终为零。`argv` 可通过 `ctx.read_at(24)` 读取（第三个参数 `const char *const *argv`）。

**修复**:
1. 从 `ctx.read_at(24)` 读取 argv 指针
2. 使用 `bpf_probe_read_user_str` 读取第一个参数（`argv[0]`）到 `event.argv` 字段
3. 数组边界：如果 `argv[0]` 失败（EFAULT），保持 `argv` 为零即可

### Task 2.3 — eBPF connect/bind 探针捕获 sockaddr 地址 🔴 Critical

**文件**: [main.rs](ebpf-probes/agentshield-ebpf/src/main.rs:89-135)

**问题**: `connect` 和 `bind` handler 的 `filename` 字段全部为零——未捕获目标地址。sockaddr 位于 `ctx.read_at(24)`（第三个参数）。

**修复**:
1. 从 `ctx.read_at(24)` 读取 `struct sockaddr *` 指针
2. 从 `ctx.read_at(16)` 读取 `addrlen`（第二个参数，`int`；connect：第三个参数？等等，需要确认）

**connect 的参数布局**（`int connect(int sockfd, const struct sockaddr *addr, socklen_t addrlen)`）:
- `ctx.read_at(16)` = `addr` (sockaddr 指针)
- `ctx.read_at(24)` = `addrlen` (socklen_t)

**bind 的参数布局**（`int bind(int sockfd, const struct sockaddr *addr, socklen_t addrlen)`）:
- 同上布局

3. 使用 `bpf_probe_read_user` 读取 `struct sockaddr` 的前几个字节来判断地址族（`sa_family`）
4. 若是 `AF_INET`（2），读取完整的 `struct sockaddr_in` 并将 IP:Port 格式化为字符串写入 `event.filename`
5. 若是 `AF_INET6`（10），标记为 `[IPv6]`（256 字节不足以格式化完整地址）
6. 若 `bpf_probe_read_user` 失败，写 `"(unknown-address)"` 到 filename

### Task 2.4 — 创建 `build-ebpf.sh` + 修复 `build-linux.sh` 🔴 Critical

**新建文件**: `ebpf-probes/build-ebpf.sh`
**修复文件**: `agent-runtime/build-linux.sh`

**新增 `ebpf-probes/build-ebpf.sh`**:
```bash
#!/bin/bash
set -euo pipefail

# Install bpf-linker if not present
cargo +nightly install bpf-linker

# Compile eBPF bytecode
cargo +nightly build -p agentshield-ebpf \
  --target bpfel-unknown-none \
  --release \
  -Z build-std=core

echo "eBPF bytecode built: target/bpfel-unknown-none/release/agentshield-ebpf"
```

**修复 `agent-runtime/build-linux.sh`**:
1. 在 eBPF 编译阶段（约第 52 行之前）添加步骤：先运行 `ebpf-probes/build-ebpf.sh`
2. 添加检查：若 eBPF 字节码文件不存在或为空，**中止构建并报错**（不要创建空占位文件）
3. 在 Docker 容器中也安装 `bpf-linker`（`cargo +nightly install bpf-linker`）

### Task 2.5 — Supervisor 消费 Hermes stdout/stderr 🟡 Medium

**文件**: [supervisor.rs](agent-runtime/src/supervisor.rs:18-27)

**问题**: Hermes 子进程的 stdout/stderr 通过 `.stdout(Stdio::piped()).stderr(Stdio::piped())` 创建，但从不对管道执行读操作。管道缓冲区满（通常 64KB）后子进程 write 会阻塞。

**修复**:
1. 在 `start()` 方法中，取出 `child.stdout` 和 `child.stderr`
2. 分别 spawn 两个 tokio task，将 stdout/stderr 转发到 tracing log（`info!` 级别），或将 stdout 读到 `/dev/null`
3. 确保 task 在子进程退出时自动终止（管道 EOF 时 task 退出）

```rust
// 示例
if let Some(stdout) = child.stdout.take() {
    tokio::spawn(async move {
        let mut reader = BufReader::new(stdout);
        let mut line = String::new();
        while reader.read_line(&mut line).await.is_ok() && !line.is_empty() {
            tracing::info!(target: "hermes", "{}", line.trim_end());
            line.clear();
        }
    });
}
```

### Task 2.6 — ProbeEvent 反序列化加验证 🟡 Medium

**文件**: [probe_manager.rs](agent-runtime/src/probe_manager.rs:188-192)

**问题**: `unsafe { ptr::read_unaligned(...) }` 直接从原始字节读取 564 字节结构体，无任何验证。结构体布局在 BPF 侧和用户态侧之间的漂移会导致静默内存破坏。

**修复**: 在 `ProbeEvent` 结构体中添加魔数字段：

1. `agentshield-ebpf-common/src/lib.rs`: 在 `ProbeEvent` 开头添加 `magic: u32 = 0xE5`（固定常量）
2. 所有 eBPF handler 中设置 `event.magic = 0xE5`
3. `probe_manager.rs` 中读事件后先检查 `magic == 0xE5`，若不匹配则 `tracing::warn!("bad ProbeEvent magic")` 并丢弃该事件

### Task 2.7 — 添加集成测试 🟡 Medium

**新建目录**: `agent-runtime/tests/`

创建以下集成测试文件：

| 文件 | 覆盖 |
|------|------|
| `tests/client_test.rs` | `register()` 重试逻辑，`heartbeat()` 响应解析，`upload_events()` 错误重入队 |
| `tests/event_buffer_test.rs` | 并发 push/drain，容量边界，push_front_batch 顺序保持 |
| `tests/probe_event_conv_test.rs` | 各种 syscall 名称的转换，resource_ref 回退，event_id 唯一性 |
| `tests/supervisor_test.rs` | start/stop/restart/is_running 生命周期 |
| `tests/integration_test.rs` | 端到端：启动 fake HTTP server → agent-runtime 注册 → heartbeat → 上传事件 |

### Task 2.8 — 杂项清理 🟢 Low

- 删除 `Cargo.toml` 中的 `tokio-test = "0.4"` dev-dependency
- 修复 `agentshield-loader/src/main.rs:96-98` 的 perf reader 空循环体（读取 available 但不处理）

---

## 协作约束（CRITICAL — 修改前必读）

### 共享契约 #5：ProbeEvent 结构体布局

`ProbeEvent` 定义于 `ebpf-probes/agentshield-ebpf-common/src/lib.rs`，**同时被 eBPF 程序（`#![no_std]`）和用户态 agent-runtime 使用**。修改此结构体时：

- **不可改变字段顺序**（影响 `#[repr(C)]` 布局）
- **只可在末尾追加字段**，且需同步更新所有 eBPF handler 中的字段写入
- **魔数字段放在第一个字段位置**（若添加魔数验证）
- **修改后必须重新编译 eBPF 字节码**（`build-ebpf.sh`）

### 共享契约 #6：上传事件格式

agent-runtime 向 Go 后端 POST 的事件格式**必须保持**：

```json
{
  "events": [{
    "event_id": "string",
    "occurred_at": "RFC3339 string",
    "family_group_id": "string",
    "agent_id": "string",
    "resource_ref": "string (filename or syscall name)",
    "action": "string (read|write|exec|network_connect|socket_create|...)",
    "attributes": {
      "comm": "process name",
      "pid": "12345",
      "uid": "1000",
      "tid": "12345"
    },
    "risk_contribution": 0.0
  }]
}
```

Stream 1 (Go) 的 `appendAuditEvents` 依赖此格式。**修改 `AuditEventPayload` 结构体前必须与 Stream 1 同步**。

### 共享契约 #7：Syscall 到 Action 映射

`probe_event_conv.rs` 中的映射关系被 Go 后端的 risk engine 和 OPA 策略使用：

| Syscall | Action |
|---------|--------|
| `openat` (read file) | `read` |
| `openat` (write/truncate) | `write` |
| `execve` | `exec` |
| `connect` | `network_connect` |
| `bind` | `socket_create` |

**不可修改这些 action 字符串**——它们被 OPA Rego 策略和 risk rules 硬编码匹配。

---

## 自我验证清单

- [ ] `cargo check --workspace` 无错误
- [ ] `cargo clippy --workspace` 无新警告
- [ ] `cargo test --workspace` 全部通过（含新增集成测试）
- [ ] `bash ebpf-probes/build-ebpf.sh` 成功生成非空 eBPF 字节码
- [ ] `bash agent-runtime/build-linux.sh` 成功生成 static-pie 二进制
- [ ] Docker 内 `file bin/agent-runtime` 确认 `ELF 64-bit ... static-pie`
- [ ] Docker 内 `strings bin/agent-runtime | grep "openat"` 确认嵌入字节码
- [ ] Docker 内运行 agent-runtime，日志显示 "4 eBPF tracepoints attached"
- [ ] 发送测试事件到管理服务器，确认 `pid`/`tid`/`uid` 字段非零

---

## 禁止事项

- ❌ **禁止修改 `ProbeEvent` 现有字段顺序**（破坏 `#[repr(C)]` 布局，eBPF 程序崩溃）
- ❌ **禁止修改 action 字符串映射**（`read`/`write`/`exec`/`network_connect`/`socket_create`）
- ❌ **禁止在 eBPF 代码中使用循环**（BPF verifier 会拒绝）
- ❌ **禁止在 eBPF handler 中分配超过 512 字节的栈空间**（使用 `PerCpuArray` BUF 代替）
- ❌ **禁止移除 `build-linux.sh` 中的 Docker 交叉编译步骤**（Windows 宿主机需要）
