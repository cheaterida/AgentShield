# 并行修复 — 主协调文件

> **目标**: 将审计发现的 24 项问题拆分到 4 个独立流中并行修复，最大化工效。
> **更新**: 2026-05-20

---
#代码规范
# CLAUDE.md

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## 四个流的终端启动命令

在每个 Claude 终端中分别执行：

### 终端 1 — Go 后端

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-1-go-backend.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行测试验证。
```

### 终端 2 — Rust / eBPF

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-2-rust-ebpf.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行测试验证。
注意：eBPF 编译需要 Docker Linux 环境，非 Linux 宿主机需通过 Docker 执行编译验证。
```

### 终端 3 — Python API 层

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-3-python-api.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行测试验证。
```

### 终端 4 — 前端 UI

```
启动新的 Claude 会话，设定工作目录为 c:\Users\Acer\Desktop\AgentShield。
请阅读 stream-4-frontend-ui.md 文件，这是你的任务计划书。
严格按照计划书中的任务清单、协作约束和验证清单执行。
修改代码前先阅读相关源文件，修改后运行测试验证。
```

---

## 流间接口合约（CRITICAL — 四个终端都必须遵守）

### 合约 A: OPA 评估归属

```
决策：OPA 评估由 Go 后端（Stream 1）负责。
- Stream 1:  保留 evaluateOPA() 在 appendAuditEvents 中的调用
- Stream 3:  移除 serve-web.py 中 _audit_events_with_opa() 的 OPA 拦截
- Stream 2/4: 不受影响
```

### 合约 B: Trace API 数据源

```
决策：serve-web.py（Stream 3）是 Trace API 唯一数据源（ClickHouse spans）。
- Stream 1:  移除 Go 端 listTraces/listSpans/ingestSpans handler
- Stream 3:  保留并维护 serve-web.py 的 _list_traces/_ingest_spans/_traces_by_agent
- Stream 4:  前端 TracesPage 无变化（已通过 Vite proxy → :8081）
```

### 合约 C: 字段命名规范

```
所有 JSON API 使用 display_name（蛇形），不使用 name。
- Go models.go:     json:"display_name"
- serve-web.py:     g.get("display_name", gid)   ← Stream 3 修复
- Frontend types:   display_name: string
- 例外: FamilyGroupWithAgents.name 保留（由 serve-web.py 从 display_name 填充）
```

### 合约 D: 审计事件属性（OPA 注入）

```
Go evaluateOPA 向 AuditEvent.attributes 注入以下字段。
Stream 4 的 SecurityEventsPage 依赖这些属性：

  opa_allow:            "true" | "false"
  opa_risk_level:       "low" | "medium" | "high" | "critical"
  opa_deny_sensitive_path: "true" (可选)
  opa_deny_network:     "true" (可选)
  opa_risky_write:      "true" (可选)

Stream 1 不可删除或重命名这些属性。
Stream 3 不可在 serve-web.py 中重新注入这些属性。
```

### 合约 E: eBPF 事件字段（agent-runtime → Go）

```
agent-runtime 上传的每个 AuditEventPayload 必须包含：

  event_id:      UUID v4
  occurred_at:   RFC3339Nano
  family_group_id:  string
  agent_id:      string
  resource_ref:  filename 或 syscall 名称
  action:        "read" | "write" | "exec" | "network_connect" | "socket_create"
  attributes.comm:  进程名 (16 bytes)
  attributes.pid:   "12345"
  attributes.uid:   "1000"     ← Stream 2 修复（当前为 0）
  attributes.tid:   "12345"    ← Stream 2 修复（当前为 0）
  risk_contribution: float64

Stream 2 不可修改 action 字符串枚举。
Stream 1 不可修改 appendAuditEvents 的请求解析格式。
```

### 合约 F: 前端构建

```
Stream 3 和 Stream 4 都会修改 web/src/ 下的文件。
共享文件 types.ts 由 Stream 3 主责，Stream 4 提交变更请求。

构建命令: npm run build --prefix management-server/web
产物目录: management-server/web/dist/

修改后必须运行 build 验证零 TypeScript 错误。
```

---

## 文件所有权矩阵

| 文件/目录 | 主责流 | 可读流 | 修改需协调 |
|-----------|--------|--------|-----------|
| `management-server/internal/**/*.go` | Stream 1 | 全部 | 接口变更通知 Stream 3/4 |
| `management-server/internal/store/migrations/` | Stream 1 | 全部 | 新增迁移文件通知全部 |
| `agent-runtime/src/**/*.rs` | Stream 2 | 全部 | 无需协调 |
| `ebpf-probes/**/*.rs` | Stream 2 | 全部 | ProbeEvent 结构体变更通知 Stream 1 |
| `agent-runtime/build-linux.sh` | Stream 2 | 全部 | 无需协调 |
| `ebpf-probes/build-ebpf.sh` | Stream 2 (新建) | 全部 | 无需协调 |
| `management-server/serve-web.py` | Stream 3 | 全部 | 响应格式变更通知 Stream 4 |
| `management-server/web/src/api/types.ts` | **Stream 3** | Stream 4 | Stream 4 提交需求给 Stream 3 |
| `management-server/web/src/api/client.ts` | Stream 3 | Stream 4 | 同上 |
| `management-server/web/vite.config.ts` | Stream 4 | Stream 3 | 简单删除，各自可改不同区域 |
| `management-server/web/src/pages/*.tsx` | **Stream 4** | Stream 3 | Stream 3 只读 |
| `management-server/web/src/context/*.tsx` | Stream 4 | Stream 3 | Stream 3 只读 |
| `management-server/web/src/App.tsx` | Stream 4 | Stream 3 | Stream 3 只读 |

---

## 合并顺序（第 1 批完成后）

```
建议合并顺序（避免冲突）：
1. Stream 1 (Go)     — 零文件冲突，先合
2. Stream 2 (Rust)   — 零文件冲突，先合
3. Stream 3 (Python) — types.ts + serve-web.py，零文件冲突（Stream 4 不碰）
4. Stream 4 (UI)     — pages/*.tsx 和 App.tsx，零文件冲突（Stream 3 不碰）

注意：4 个流修改的文件集合完全不相交。并行执行无 git merge 冲突风险。
```

---

## 流间依赖与阻塞关系

```
第 1 批（全部并行，0 阻塞）:
  Stream 1 ████████████████ (Go: gRPC, test, healthz, traces)
  Stream 2 ████████████████ (Rust: eBPF data, build, supervisor, tests)
  Stream 3 ████████████████ (Python: OPA, traces, fields, types)
  Stream 4 ████████████████ (UI: error states, alerts, ws, dead code)

第 2 批（第 1 批全完成后）:
  Stream 4: PoliciesPage Activate button (依赖 Stream 3 的 client.ts 修改)
  Stream 2: 集成测试 (依赖 Stream 2 自身修复完成)

无跨流阻塞依赖。
```

---

## 公共参考文档

每个终端应阅读以下文件了解上下文：

1. **`CLAUDE.md`** — 项目架构、构建命令、组件说明
2. **`development-progress.md`** — 完整开发进度 + 审计报告（第六、七节）
3. **各自的任务计划书**（`stream-N-*.md`）
4. 相关源代码文件（计划书中列出的文件路径）

---

## 协调人（你）的检查点

当所有 4 个流完成后，执行以下验证：

```bash
# 1. 编译验证
make check                                    # proto-gen + build-management + build-frontend
cargo check --workspace                       # Rust 编译
cargo clippy --workspace                      # Rust lint

# 2. 测试验证
make test                                     # test-management + test-agent-runtime
npm run build --prefix management-server/web  # TypeScript + Vite build

# 3. 服务启动验证
# 终端 A: python management-server/serve-web.py
# 终端 B: 启动 management-server Go 二进制
# 终端 C: 浏览 http://localhost:8081

# 4. 功能验证
# - 所有 8 个页面加载正常
# - 停止 Go 后端 → 每个页面显示红色错误卡片（非永久 spinner）
# - 重启 Go 后端 → 点重试 → 数据恢复
# - WebSocket 断开/重连 → 侧边栏状态切换
# - AlertsPage 状态四种颜色可区分
# - Trace 页面侧边栏显示 display_name
# - gRPC :9090 端口不再监听
# - healthz 端点正常返回
```

---

## 紧急情况处理

**如果某个流发现了影响其他流的问题：**
1. 在该流的任务计划书文件末尾追加 "跨流发现" 节
2. 通过 git commit message 标记 `CROSS-STREAM:` 前缀
3. 协调人检查后通知其他流

**如果两个流意外修改了同一文件：**
- `types.ts`：以 Stream 3 为准，Stream 4 的修改在 Stream 3 之上 reapply
- `client.ts`：以 Stream 3 为准
- 其他文件：按 git merge 正常处理（预计不会发生，文件集不相交）

---

## 流文件索引

| 文件 | 用途 |
|------|------|
| [stream-1-go-backend.md](stream-1-go-backend.md) | 终端 1: Go 后端 Bug 修复 |
| [stream-2-rust-ebpf.md](stream-2-rust-ebpf.md) | 终端 2: Rust/eBPF 数据捕获与构建 |
| [stream-3-python-api.md](stream-3-python-api.md) | 终端 3: serve-web.py + 前端 API 层 |
| [stream-4-frontend-ui.md](stream-4-frontend-ui.md) | 终端 4: 前端 UI 健壮性 |
| [development-progress.md](development-progress.md) | 审计报告（上下文参考） |
| [CLAUDE.md](CLAUDE.md) | 项目架构与构建命令 |
