# Stream 2B: Supervisor stdout/stderr + build-linux.sh 修复

> **终端标识**: `stream-2b-supervisor-build`
> **优先级**: 🟡 Medium (Supervisor) + 🔴 Critical (build-linux.sh)
> **依赖**: 无外部依赖，可立即开工
> **预计工时**: 1-2h

---

## 任务清单

### Task 2.5 — Supervisor 消费 Hermes stdout/stderr（🟡 Medium）

**文件**: `agent-runtime/src/supervisor.rs`

**问题**: Hermes 子进程的 stdout/stderr 通过 `.stdout(Stdio::piped()).stderr(Stdio::piped())` 创建，但从不读取管道。管道缓冲区满（64KB）后子进程 write 阻塞。

**修复步骤**:

1. 阅读 `supervisor.rs` 的 `start()` 方法，找到创建子进程的位置
2. 在 `.spawn()` 之后取出 `child.stdout` 和 `child.stderr`
3. 对 stdout 和 stderr 分别 spawn tokio task 做消费：
   - stdout: 使用 `BufReader::read_line()` 逐行读取，通过 `tracing::info!(target: "hermes", "{}", line)` 转发到日志
   - stderr: 同样模式，使用 `tracing::warn!(target: "hermes", "{}", line)`
4. 确保 task 在管道 EOF 时自然退出

```rust
// 示例：在 child.spawn() 之后
if let Some(stdout) = child.stdout.take() {
    tokio::spawn(async move {
        let reader = tokio::io::BufReader::new(stdout);
        let mut lines = reader.lines();
        while let Ok(Some(line)) = lines.next_line().await {
            tracing::info!(target: "hermes", "{}", line);
        }
    });
}
if let Some(stderr) = child.stderr.take() {
    tokio::spawn(async move {
        let reader = tokio::io::BufReader::new(stderr);
        let mut lines = reader.lines();
        while let Ok(Some(line)) = lines.next_line().await {
            tracing::warn!(target: "hermes", "{}", line);
        }
    });
}
```

**注意**:
- `BufReader` 和 `lines()` 来自 `tokio::io`（不是 `std::io`）
- 需要确认 `tokio` 的 `io-util` feature 已启用（检查 `Cargo.toml`）

---

### Task 2.4 — 修复 `build-linux.sh`（🔴 Critical）

**文件**: `agent-runtime/build-linux.sh`

**当前问题**:
1. eBPF 编译在脚本中缺失 — 脚本复制了源代码但没有在 Docker 内编译
2. 第 51-58 行：eBPF 字节码不存在时创建空占位文件（`touch`）— 应该在编译 eBPF 字节码后将其复制进构建上下文
3. Docker 内 `rust:1.87-alpine` → 需要 LLVM（`apk add llvm-dev`）来运行 `bpf-linker`
4. 需要 `musl-dev` 用于 agent-runtime musl 编译

**修复步骤**:

1. **在脚本开头添加 eBPF 编译步骤**（在创建 BUILD_DIR 之前）:
   ```bash
   # Step 0: Build eBPF bytecode first
   echo "=== Building eBPF bytecode ==="
   bash ebpf-probes/build-ebpf.sh
   ```

   注意：`build-ebpf.sh` 已在 Task 2.2 中创建，位于 `ebpf-probes/build-ebpf.sh`。检查其存在性。

2. **修复第 51-58 行**（字节码复制逻辑）:
   - 从 `touch`（创建空文件）改为 `exit 1`（中止构建）
   - 改为：
   ```bash
   if [ -f ../target/bpfel-unknown-none/release/agentshield-ebpf ]; then
     echo "Using real eBPF bytecode ($(du -h ...))"
     cp ... "$BUILD_DIR/target/..."
   else
     echo "ERROR: eBPF bytecode not found at ..."
     echo "Run: bash ebpf-probes/build-ebpf.sh"
     exit 1
   fi
   ```

3. **更新 Docker 镜像**（第 90 行）:
   - 从 `rust:1.87-alpine` 改为 `rust:1.91-alpine`（避免 rustc 版本过旧问题：`cargo-platform` 需要 rustc ≥1.91）
   - 添加构建依赖: `apk add --no-cache musl-dev linux-headers llvm-dev`

4. **在 Docker 内编译 eBPF**（Docker sh -c 体内）:
   同在 Docker 内安装 `bpf-linker` 并编译 eBPF（确保字节码存在）：
   ```bash
   cargo +nightly install bpf-linker
   cargo +nightly build -p agentshield-ebpf --target bpfel-unknown-none --release -Z build-std=core
   ```

5. **检查二进制结果**:
   确认 `file bin/agent-runtime` 输出包含 `static-pie`。

---

## 编译验证

### Cargo 缓存加速

`-v "agent-shield-cargo:/usr/local/cargo"` 将工具链和 crate 依赖缓存于 Docker volume。首次编译 ~10 min（安装 musl-tools + 下载依赖），后续仅需 ~30s（增量编译）。

如果 volume 已被其他终端初始化过，跳过依赖下载阶段，直接编译变更代码。

```powershell
# 1. 验证 agent-runtime 编译
docker run --rm `
  -v "C:\Users\Acer\Desktop\AgentShield:/build" `
  -v "agent-shield-cargo:/usr/local/cargo" `
  -w /build `
  rust:1.91-bookworm sh -c "
    apt-get update -qq && apt-get install -y -qq musl-tools 2>&1 | tail -1
    cargo build -p agent-runtime --target x86_64-unknown-linux-musl --release 2>&1
    ls -lh /build/target/x86_64-unknown-linux-musl/release/agent-runtime
"

# 2. 测试 build-linux.sh（需要 Docker Desktop）
bash agent-runtime/build-linux.sh
```

---

## 文件所有权

| 文件 | 你修改 | 其他流修改 |
|------|--------|-----------|
| `supervisor.rs` | ✅ | 无 |
| `build-linux.sh` | ✅ | 无 |

## 协作约束

- ❌ 禁止修改 `supervisor.rs` 中的 `start()` 方法签名
- ❌ 禁止修改 `build-linux.sh` 的输出路径（`bin/agent-runtime`）
- ❌ 禁止移除 Docker 交叉编译步骤（Windows 宿主机需要 Docker）
