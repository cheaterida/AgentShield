# Stream 2C: 集成测试 + 杂项清理

> **终端标识**: `stream-2c-tests-cleanup`
> **优先级**: 🟡 Medium (tests) + 🟢 Low (cleanup)
> **依赖**: 依赖 Stream 2A 和 2B 的代码改动完成（测试需要正确的 handler 和 supervisor 行为），但测试文件本身可独立编写
> **预计工时**: 2-3h

---

## 任务清单

### Task 2.7 — 添加集成测试（🟡 Medium）

**新建目录**: `agent-runtime/tests/`

创建以下 5 个测试文件：

#### 2.7.1 `tests/client_test.rs` — HTTP Client 测试

覆盖 `src/client.rs` 的关键函数：
- `register()` 重试逻辑（模拟 HTTP 503 → 重试 → 成功）
- `heartbeat()` 响应解析
- `upload_events()` 错误重入队

```rust
// 测试框架建议：使用 mockito 或 wiremock 启动本地 HTTP mock server
// 如果 mockito 过于重量，可以写单元级别的逻辑测试
#[tokio::test]
async fn test_register_retry_on_503() { /* ... */ }
#[tokio::test]
async fn test_heartbeat_ok() { /* ... */ }
#[tokio::test]
async fn test_upload_events_retry_on_failure() { /* ... */ }
```

#### 2.7.2 `tests/event_buffer_test.rs` — EventBuffer 并发测试

覆盖 `src/event_buffer.rs`：
- 并发 push/drain
- 容量边界（满时 push 行为）
- `push_front_batch` 顺序保持

```rust
#[tokio::test]
async fn test_concurrent_push_drain() { /* ... */ }
#[tokio::test]
async fn test_capacity_boundary() { /* ... */ }
#[tokio::test]
async fn test_push_front_batch_order() { /* ... */ }
```

#### 2.7.3 `tests/probe_event_conv_test.rs` — 事件转换测试

覆盖 `src/probe_event_conv.rs`：
- 各种 syscall 名称的转换
- resource_ref 回退（filename 空时使用 syscall 名）
- event_id 唯一性（两次转换产生不同 event_id）

```rust
#[test]
fn test_openat_read_conversion() { /* ... */ }
#[test]
fn test_execve_conversion() { /* ... */ }
#[test]
fn test_connect_conversion() { /* ... */ }
#[test]
fn test_empty_filename_fallback() { /* ... */ }
#[test]
fn test_event_id_uniqueness() { /* ... */ }
```

注意：Task 2.6 加入了 `magic` 字段。构造 `ProbeEvent` 时需要设置 `magic: 0xE5`。

#### 2.7.4 `tests/supervisor_test.rs` — 生命周期测试

覆盖 `src/supervisor.rs`：
- start/stop/restart/is_running 生命周期
- 子进程退出后的检测

```rust
#[tokio::test]
async fn test_supervisor_start_stop() { /* ... */ }
#[tokio::test]
async fn test_supervisor_restart() { /* ... */ }
#[tokio::test]
async fn test_supervisor_is_running() { /* ... */ }
```

#### 2.7.5 `tests/integration_test.rs` — 端到端测试

端到端：启动 fake HTTP server → agent-runtime 注册 → heartbeat → 上传事件。

```rust
#[tokio::test]
async fn test_e2e_register_heartbeat_upload() { /* ... */ }
```

---

### Task 2.8 — 杂项清理（🟢 Low）

两个独立的琐碎修复：

#### 2.8.1 删除不需要的 dev-dependency

**文件**: `Cargo.toml`（仓库根）

检查 agent-runtime 的 `Cargo.toml`（不是根 `Cargo.toml`，是 `agent-runtime/Cargo.toml`）中的 `[dev-dependencies]`：

读 `agent-runtime/Cargo.toml`，查找 `tokio-test = "0.4"`。如果存在且未使用，删除。

如果不存在或正在被测试使用（你刚写了测试！），则保留。

#### 2.8.2 修复 loader 空循环体

**文件**: `ebpf-probes/agentshield-loader/src/main.rs`

第 96-98 行附近的 perf reader 循环：读取 `available` 但不处理。

读文件找到空循环体，确认是否已经被修复。如果仍然存在，修复为读取并丢弃（或打印调试信息）。

```rust
// 修复前（空循环体）:
while reader.read(&mut [])? > 0 {}

// 修复后:
let mut buf = [0u8; 4096];
while reader.read(&mut buf)? > 0 {
    // 丢弃 — loader 仅用于测试，不处理事件
}
```

---

## 编译验证

### Cargo 缓存加速

`-v "agent-shield-cargo:/usr/local/cargo"` 将工具链和 crate 依赖缓存于 Docker volume。首次编译 ~10 min，后续 ~30s。

```powershell
# 运行所有 agent-runtime 测试（含新增集成测试）
docker run --rm `
  -v "C:\Users\Acer\Desktop\AgentShield:/build" `
  -v "agent-shield-cargo:/usr/local/cargo" `
  -w /build `
  rust:1.91-bookworm sh -c "
    apt-get update -qq && apt-get install -y -qq musl-tools 2>&1 | tail -1
    cargo test -p agent-runtime 2>&1
"
```

**验证标准**:
- [ ] 所有新测试通过
- [ ] `cargo clippy -p agent-runtime` 无新警告
- [ ] `cargo clippy -p agentshield-loader` 无新警告

---

## 文件所有权

| 文件 | 你修改 | 其他流修改 |
|------|--------|-----------|
| `agent-runtime/tests/*.rs` | ✅ 新建 | 无 |
| `agent-runtime/Cargo.toml` | ✅ | 无 |
| `ebpf-probes/agentshield-loader/src/main.rs` | ✅ | 无 |

## 协作约束

- ❌ 禁止修改 `ProbeEvent` 结构体
- ❌ 禁止修改已完成的任务文件（`main.rs`、`supervisor.rs`、`probe_manager.rs`）
- ✅ 测试可以导入 crate 内部模块（使用 `use agent_runtime::...` 需要模块是 `pub`）
- ✅ 如果测试需要 mock，可使用 `mockito` 或 `wiremock`（添加到 `[dev-dependencies]`）
