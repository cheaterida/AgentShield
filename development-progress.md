# AgentShield 开发进度报告

> 基于设计文档 `design.md` 与当前源代码（截至 2026-05-20）的对比分析，含全组件代码审计。

---

## 总体概览

| 维度 | 内容 |
|------|------|
| **项目阶段** | Alpha — 核心骨架已完成，部分高级功能尚待开发 |
| **代码规模** | Go (管理端+网关+错误处理)、Rust (运行时+eBPF)、Python (ML管道+桥接+SDK)、TypeScript/React (前端) |
| **核心设计语言** | 设计稿指定 **Python + FastAPI** 后端；实际采用 **Go** 作为主力后端语言，Python 仅用于 ML 管道 |
| **核心设计前端** | 设计稿指定 **Vue3 + ECharts**；实际采用 **React + recharts + TypeScript** |
| **数据库选型** | 设计稿指定 MySQL；实际采用 SQLite（PostgreSQL 已列入计划） |

---

## 一、功能实现对比矩阵

### 1. 智能体资产全生命周期管理模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| Agent ID + 部门/角色/安全等级绑定 | `family_group_id` + `labels` (JSON) | ✅ **已实现** | Go 模型层 `agents` 表支持；API: `POST /api/v1/agents/register` |
| 未备案"影子智能体"预警 | 未实现 | ❌ **未实现** | 无相关检测逻辑 |
| 三级管理体系 (部门/安全团队/CISO) | OPA `risk_level`: low/medium/high/critical | 🔄 **部分实现** | 通过 OPA Rego 策略分级，非三级审批流形式 |
| 可视化看板 | React Dashboard 页面 | ✅ **已实现** | 使用 recharts 图表库，非设计稿的 ECharts |
| 远程启停/隔离/熔断 | Agent 状态控制 + error-handler 断路器 | ✅ **已实现** | Agent 状态: active/suspicious/degraded/inactive；error-handler 4 级响应 |

### 2. 运行时行为实时感知模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| Middleware Hook 探针 | **eBPF 内核探针**（4 个 tracepoint） | ✅ **已实现** | 非中间件钩子，而是内核级 eBPF 探针（openat/execve/connect/bind） |
| NetworkX CFG 路径建模 | **DGL（Deep Graph Library）CFG 构建器** | 🔄 **技术替换** | 设计稿用 NetworkX，实际用 DGL 以支持后续 GNN 训练 |
| PyTorch 150D 语义向量 | **Contrastive Autoencoder + TransformerEncoder** | ✅ **已实现** | 150 维潜在空间：action(28D)+resource(42D)+attr(30D)+temporal(10D)+agent(20D)+residual(20D) |
| FAISS 余弦相似度检索 | **未实现** | ❌ **未实现** | 150D 向量已可生成，但无数库索引/检索基础设施 |
| CFG 状态机路径校验 | **Go 端 CFG 评分 + Python 端 GNN 异常检测** | 🔄 **部分实现** | 存在 CFG 异常评分能力，但非实时控制流图路径校验 |

### 3. 风险评估与智能决策模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| 多维度量化评分 | 3 条规则 + EMA 平滑 + 可选 ML 混合 | ✅ **已实现** | 敏感路径(0.5)+写操作(0.2)+网络访问(0.2-0.3)；EMA alpha=0.3；ML 权重从 0.1 动态增至 0.7 |
| AttackAgent 自动化红队 | **未实现** | ❌ **未实现** | 无红队模拟或攻击模式自动提取模块 |

### 4. 柔性防御响应模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| 欺骗式防御（Mock 脱敏） | **未实现** | ❌ **未实现** | 无数据脱敏或虚假凭证返回机制 |
| COW 写时复制快照 | **未实现** | ❌ **未实现** | 无增量快照或断点续跑能力 |
| 异常熔断自动回滚 | error-handler `Rollback` / `Degrade` / `CircuitBreak` | 🔄 **部分实现** | 管理级回滚（改状态+确认告警），非 COW 快照回滚 |

### 5. 权限资源管控模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| 双域统一权限映射（IAM） | **未实现** | ❌ **未实现** | 无 IAM 集成 |
| ABAC 动态管理（JIT 即用即收） | **未实现** | ❌ **未实现** | 无 JIT 权限申请/回收 |
| Token 算力配额管理 | **未实现** | ❌ **未实现** | 无 Token 消耗监控 |

### 6. 内核级安全底座模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| LSM/SELinux MAC 强制裁决 | SELinux `.te` 策略占位文件 | 🔄 **仅骨架** | `agentshield.te` 仅声明 `policy_module`，无实际 allow 规则 |
| MAC 文件安全回收站 | **未实现** | ❌ **未实现** | 无文件隔离/召回机制 |

### 7. 可视化管控与审计模块

| 设计文档要求 | 实际实现 | 状态 | 说明 |
|-------------|---------|------|------|
| Trace ID 全链路溯源 | audit_events 表 + event_id / trace_id | ✅ **已实现** | 事件可追溯；全链路追踪链正在建设 |
| Vue3 + ECharts 态势感知大屏 | **React + recharts + TypeScript** | 🔄 **技术替换** | 技术栈不同，功能基本覆盖：Dashboard、Agents、Alerts、Audit Log |
| WebSocket 实时推送 | Gorilla WebSocket Hub | ✅ **已实现** | 实时推送 `audit_event` 和 `risk_alert` 两种事件类型 |
| 全角色交互（普通用户+管理员） | React SPA 多页面 | ✅ **已实现** | 7 个页面覆盖仪表盘、智能体、告警、审计、策略、家庭组 |

---

## 二、实际实现优于设计的亮点

### 1. 链路追踪 SDK (`sdk/python/agentshield_tracer.py`)
设计文档未提及，实际实现了：
- `trace_llm_call()` 上下文管理器 —— 手动追踪 LLM 调用
- `wrap_openai()` —— OpenAI 客户端自动埋点
- 自动捕获 Prompt/Completion/Token 用量
- 兼容 OpenTelemetry 格式，支持后续集成标准观测平台

### 2. ClickHouse 桥接服务 (`bridge/langtrace_bridge.py`)
设计文档未提及，实际实现了：
- 基于 checkpoint 的增量轮询 ClickHouse `agentshield.spans` 表
- Span → AuditEvent 格式转换
- 无第三方依赖（仅 requests），轻量部署

### 3. 边缘网关安全 (`gateway/` + Envoy)
- **HMAC-SHA256 签名验证** —— Webhook 请求防篡改
- **Token-bucket 速率限制** —— 租户级每秒 100 请求
- **Envoy TLS 终结** —— 端口 8443 SSL 卸载
- 设计文档未涉及边缘网关安全层

### 4. 四层级错误处理与熔断 (`error-handler/`)
- **Low** → 聚合（5 次/5 分 → 升级 Critical）
- **Medium** → 回滚（确认告警）
- **High** → 降级（Agent → suspicious）
- **Critical** → 熔断（Agent → degraded）
- 设计文档仅提到"熔断"，未设计如此精细的分级响应

### 5. gRPC 服务定义 (`shared/proto/agentshield/v1/`)
- 5 个 proto 文件，26 个 RPC 定义
- 覆盖 FamilyGroup/Agent/Heartbeat/Audit/Policy/Alert/Dashboard 全量 CRUD
- 设计文档未提及 gRPC

### 6. OPA 细粒度策略引擎
- `audit.rego` —— 运行时行为审计（敏感路径/网络/写操作检测）
- `authz.rego` —— 准入控制（鉴权/速率限制/风险分级）
- 设计文档未提及 OPA/Rego 策略引擎

---

## 三、技术栈差异汇总

| 层次 | 设计稿推荐 | 实际采用 | 差异说明 |
|------|-----------|---------|---------|
| 后端主语言 | Python + FastAPI | **Go 1.22/1.25** | Go 替代 Python 作为主力后端，Python 仅用于 ML |
| 前端框架 | Vue3 + WebGL | **React 18 + TypeScript** | 完全不同的前端技术栈 |
| 图表库 | ECharts | **recharts** | React 生态原生图表组件 |
| 关系数据库 | MySQL | **SQLite**（+ PostgreSQL 规划） | 轻量化先行，满足 SME 场景 |
| 缓存 | Redis | **未实现** | 审计事件使用内存环形缓冲区，无独立缓存层 |
| CFG 库 | NetworkX | **DGL** | DGL 支持 GPU 加速图神经网络训练 |
| 向量检索 | FAISS | **未实现** | 150D 向量已生成，但未建索引 |
| 容器化 | Docker + Ubuntu | **Docker Compose（7 配置）** | 支持 8 个服务，7 个 profile 配置 |

---

## 四、实现状态全景图

### ✅ 已完成（可直接使用）

| 功能模块 | 涉及组件 |
|---------|---------|
| Agent 注册/心跳/状态管理 | management-server, agent-runtime |
| 审计事件采集与存储 | agent-runtime, management-server, SQLite |
| eBPF 内核探针（4 个 tracepoint） | ebpf-probes (Rust/Aya) |
| 规则引擎评分（3 规则 + EMA） | management-server `internal/risk/` |
| OPA 策略评估（审计+准入） | security-policy, management-server |
| WebSocket 实时推送 | management-server `internal/api/websocket.go` |
| React 管理面板（7 页面） | management-server/web |
| HMAC 安全网关 | gateway |
| 4 级错误处理与熔断 | error-handler |
| CAE 150D 行为语义编码 | ml-pipeline (PyTorch) |
| GAT-GNN 异常检测 | ml-pipeline (DGL + PyTorch) |
| CFG 图构建 | ml-pipeline |
| Python SDK 追踪器 | sdk/python |
| ClickHouse 桥接 | bridge |
| gRPC 定义 + Go 桩代码 | shared/proto |

### 🔄 部分实现

| 功能模块 | 现有内容 | 缺失内容 |
|---------|---------|---------|
| SELinux 内核加固 | `.te` 文件骨架 | 完整 MAC 策略规则 |
| GNN 异常检测 | 模型代码 + 训练脚本 | 生产级预训练模型 checkpoint |
| CFG 路径校验 | DGL 图构建 + 评分 API | 实时在线路径偏离检测 |
| 分级管控 | OPA risk_level | 完整的三级审批工作流 |
| 异常熔断回滚 | API 级状态变更 | COW 快照回滚 |

### ❌ 未启动

| 功能模块 | 优先级预估 |
|---------|-----------|
| FAISS 向量检索与行为基线 | 高 |
| AttackAgent 自动化红队 | 中 |
| 欺骗式防御（Mock 数据脱敏） | 中 |
| COW 写时复制快照与断点续跑 | 低 |
| 企业 IAM 双域权限映射 | 高 |
| ABAC JIT 即用即收权限 | 高 |
| Token 算力配额管理 | 中 |
| MAC 文件安全回收站 | 低 |
| Redis 缓存层 | 中 |
| 影子智能体检测 | 中 |

---

## 五、当前架构数据流（实际）

```
┌─────────────┐     ┌──────────┐     ┌──────────────────┐
│   SDK (Python) │────▶│  Gateway  │────▶│ Management Server │
│  trace_llm_call│     │  HMAC    │     │  Go :8080/:9090   │
│  wrap_openai   │     │  RateLimit│     │  REST + gRPC      │
└───────────────┘     └──────────┘     └────────┬─────────┘
       │                                         │
       │  POST /api/v1/spans                     │  OPA /v1/data
       ▼                                         ▼
┌──────────────────┐                    ┌──────────────────┐
│    ClickHouse    │                    │  OPA (Rego)      │
│  agentshield.spans │                    │  audit + authz   │
└────────┬─────────┘                    └──────────────────┘
         │
         │  bridge poll
         ▼
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│  Bridge (Python)  │────▶│  ML Pipeline     │◀────│  Error Handler   │
│  span→audit event │     │  FastAPI :8090   │     │  Go tiered resp  │
└──────────────────┘     │  CAE 150D + GNN   │     └──────────────────┘
                         │  CFG analysis     │
                         └──────────────────┘

┌──────────────────────────────────────────────┐
│  Agent Runtime (Rust)                        │
│  ┌──────────┐  ┌──────────┐  ┌────────────┐ │
│  │ eBPF     │─▶│ Event    │─▶│ Heartbeat  │ │
│  │ Probes   │  │ Buffer   │  │ + Upload   │ │
│  └──────────┘  └──────────┘  └────────────┘ │
└──────────────────────────────────────────────┘
```

---

## 六、后续开发建议（2026-05-20 审计后更新）

### P0 — 阻断性 Bug（立修，阻塞 CI/功能）

| # | 问题 | 位置 |
|---|------|------|
| 1 | gRPC 服务器零 handler 注册 — 实现或移除 | `internal/grpc/server.go:50-55` |
| 2 | `engine_test.go` 编译错误 — `checkThresholds` 签名不匹配 | `internal/risk/engine_test.go:178` |
| 3 | eBPF 探针 tid/uid/net_addr 未捕获 — 安全事件数据不完整 | `ebpf-probes/agentshield-ebpf/src/main.rs` |
| 4 | eBPF 字节码无构建脚本 — 需 `build-ebpf.sh` + bpf-linker | `agent-runtime/build-linux.sh:52-58` |
| 5 | OPA 双重评估（serve-web.py + Go）→ 重复告警 | `serve-web.py:276` + `router.go:269` |

### P1 — 接口对齐（本周，消除功能模糊）

| # | 问题 | 位置 |
|---|------|------|
| 6 | Trace API 双数据源格式不兼容 — serve-web.py vs Go 统一 | `serve-web.py:467` vs `router.go:509` |
| 7 | `healthz` 路径 404 — 前端 `/api/v1/healthz` vs Go `/healthz` | `client.ts:17` vs `router.go:30` |
| 8 | serve-web.py 字段名 `name` vs Go `display_name` — 侧边栏显示 ID | `serve-web.py:604` |
| 9 | `AlertsPage` 用 SeverityBadge 显示 status — 4 状态同色不可区分 | `AlertsPage.tsx:73` |
| 10 | 8/9 前端页面无错误状态 — fetch 失败静默 | 除 TracesPage 外全页面 |

### P2 — 代码质量（下周，消债）

| # | 问题 | 位置 |
|---|------|------|
| 11 | WebSocket 无 onerror/ping/mounted guard — leak + 假连接 | `WebSocketContext.tsx` |
| 12 | Hermes stdout/stderr 管道无消费者 — 满 buffer 后子进程阻塞 | `supervisor.rs:19-21` |
| 13 | ProbeEvent unsafe 反序列化无验证 — 结构体漂移即内存破坏 | `probe_manager.rs:188-192` |
| 14 | agent-runtime 无集成测试 — client/supervisor/probe 均未覆盖 | 多个文件 |
| 15 | 死代码清理：SecurityEvent、SpanSummary、Activity import、/ws proxy、HeartbeatService、独立 traces HTML | 6 处 |

### P3 — 功能增强（后续迭代）

| # | 问题 |
|---|------|
| 16 | FAISS 向量检索引擎 — 行为基线余弦相似度 |
| 17 | 企业 IAM 集成 + ABAC JIT 权限 |
| 18 | SELinux MAC 完整策略 — 完成 `agentshield.te` |
| 19 | PoliciesPage 加 Activate 按钮 |
| 20 | AttackAgent 自动化红队 |
| 21 | Redis 缓存层 |
| 22 | MySQL/PostgreSQL 生产就绪 |
| 23 | Token 算力配额管理 |
| 24 | 全链路追踪拓扑图 |

---

---

## 七、全组件代码审计报告（2026-05-20）

> 对 management-server（Go）、agent-runtime + eBPF（Rust）、前端（React/TypeScript）、serve-web.py（Python 代理）进行了逐文件审计。聚焦接口对齐、功能模糊、死代码和架构冲突。

### 7.0 审计总览

| 组件 | 文件数 | 代码行数（估） | 严重问题 | 中等问题 | 轻微问题 |
|------|--------|--------------|---------|---------|---------|
| management-server (Go) | 25+ | ~3500 | 2 | 2 | 1 |
| agent-runtime + eBPF (Rust) | 15+ | ~2000 | 4 | 2 | 3 |
| React Frontend (TS) | 21 | ~3000 | 1 | 5 | 3 |
| serve-web.py (Python) | 1 | 1060 | 2 | 1 | 1 |
| 跨组件接口 | — | — | 3 | 2 | 1 |

### 7.1 严重问题（Critical）

#### C1. gRPC 服务完全未注册 — 21 个 RPC 方法全部不可用

**文件**: [server.go](management-server/internal/grpc/server.go:50-55)

gRPC 服务器在 `:9090` 启动并监听，但**零个**服务处理器被注册。代码显式注释掉了 `RegisterManagementServiceServer` 调用。`HeartbeatService` 结构体及 `ProcessHeartbeat` 方法已定义但从未被实例化或注册——纯死代码。

**影响**：`shared/proto/` 中 5 个 proto 文件定义的 21 个 Unary RPC + 2 个 Streaming RPC 均返回 `Unimplemented`。

**修复建议**：实现 `ManagementServiceServer` 接口（可逐方法添加），或移除 gRPC 服务器启动代码以消除误导。

#### C2. 单元测试编译错误

**文件**: [engine_test.go](management-server/internal/risk/engine_test.go:178)

```go
alerts := eng.checkThresholds("agent-x", "fg", tc.ema)
```

实际函数签名为 `func (e *Engine) checkThresholds(ev *models.AuditEvent, ema float64) *models.RiskAlert`：
- 测试传 3 个参数 (string, string, float64)；函数接受 2 个 (*AuditEvent, float64)
- 测试期望返回 `[]models.RiskAlert`；函数返回 `*models.RiskAlert`（单指针非切片）

**影响**：`go test ./...` 无法通过编译，CI 会失败。

#### C3. eBPF 探针数据捕获不完整

**文件**: [main.rs](ebpf-probes/agentshield-ebpf/src/main.rs:29-135)

所有 4 个 tracepoint handler 存在以下问题：
- `tid` 始终硬编码为 `0`（未从 `bpf_get_current_pid_tgid()` 提取线程 ID）
- `uid` 始终硬编码为 `0`（未从 `bpf_get_current_uid_gid()` 提取）
- `retval` 始终硬编码为 `0`（`sys_enter` 在 syscall 执行前触发，返回值此时确实不存在——但字段名具有误导性）
- `execve` handler 不捕获 `argv`（`ctx.read_at(24)` 可读取第三个参数但被忽略）
- `connect`/`bind` handler 的 `filename` 字段全部填 0（未捕获 sockaddr 地址数据）

**影响**：安全事件页面的"进程"、"PID"、"UID"列只有 PID 有效；无法按 UID 审计；网络事件无法显示目标 IP:Port。

#### C4. eBPF 字节码无构建脚本

**文件**: [build-linux.sh](agent-runtime/build-linux.sh:52-58)

`build-linux.sh` 不编译 eBPF 字节码——它从 `../target/bpfel-unknown-none/release/agentshield-ebpf` 复制**预构建**的二进制。若文件不存在，创建空占位文件，导致运行时段错误。脚本中也没有安装 `bpf-linker` 的步骤（GitHub Actions workflow `build-agent.yml:82` 有此步骤）。

**影响**：若未单独构建 eBPF 字节码，agent-runtime 编译成功但运行时段错误——静默退化为 demo 模式。

#### C5. OPA 双重评估

**文件**: [serve-web.py](management-server/serve-web.py:276-347), [router.go](management-server/internal/api/router.go:269)

`POST /api/v1/audit/events` 的审计事件被评估两次：
1. **serve-web.py** `_audit_events_with_opa()` 拦截请求，评估 OPA 策略，注入属性，转发给 Go
2. **Go 后端** `appendAuditEvents()` 再次调用 `r.evaluateOPA()` 重新评估相同事件

**影响**：生成重复的 OPA 告警记录；风险评分不一致的可能；OPA 服务器负载翻倍。

#### C6. serve-web.py 与 Go 后端 Trace API 响应格式不兼容

Go `listTraces`（router.go:509-583）返回 `{first_seen, last_seen, model, action, duration_ms, occurred_at}`。serve-web.py `_list_traces`（serve-web.py:467-501）返回 `{earliest, latest, name, kind, start_time, end_time, duration, status_code, attributes, events}`。前端类型匹配 serve-web.py 格式，Vite proxy 将 `/api/v1/traces` 路由到 serve-web.py `:8081`。**但如果 proxy 规则变更或 serve-web.py 未运行**，Go 后端响应格式与前端类型不兼容，页面静默失败。

### 7.2 接口对齐与功能模糊问题

#### I1. serve-web.py 字段名映射断裂

serve-web.py `_family_groups_with_agents()` 使用 `g.get("name", gid)` 读取群组名称（line 604），但 Go 模型字段为 `display_name`（models.go:57）。类似地，Agent 的 `name`/`hostname` 在 Go 模型中不存在——Go 使用 `display_name`。serve-web.py 靠 fallback 到 ID 来掩盖此问题。

**影响**：从 serve-web.py 透传时，TracesPage 侧边栏中群组名和 Agent 名始终显示为 ID，不是人类可读名称。

#### I2. `healthz` API 路径错误

**前端** [client.ts:17](management-server/web/src/api/client.ts#L17) 请求 `GET /api/v1/healthz`（BASE=`/api/v1` + `/healthz`）。**Go 后端** [router.go:30](management-server/internal/api/router.go#L30) 注册 `GET /healthz`（无前缀）。该端点会 **404**。当前无页面调用此方法，属于潜伏 bug。

#### I3. 前端 `AuditEvent` 属性访问无类型安全

`SecurityEventsPage` 通过 `ev.attributes?.opa_risk_level`、`ev.attributes?.comm`、`ev.attributes?.pid` 访问 eBPF 事件字段（[SecurityEventsPage.tsx:171-175](management-server/web/src/pages/SecurityEventsPage.tsx#L171-L175)），但 `attributes` 类型为 `Record<string, string>`。这些字段在编译期无保障——字段名拼写错误或后端变更格式时静默失败。

#### I4. `AlertsPage` 严重性/状态显示混淆

[AlertsPage.tsx:73](management-server/web/src/pages/AlertsPage.tsx#L73)：`<SeverityBadge severity={a.status} />` — 使用严重性颜色映射来显示状态字段。`status` 值为 `open`/`acknowledged`/`resolved`/`dismissed`，全部落入 `SeverityBadge` 的 default 分支（蓝色），四种状态不可区分。

#### I5. Vite proxy `/ws` 规则死代码

[vite.config.ts:14-17](management-server/web/vite.config.ts#L14-L17) 配置了 `'/ws'` 的 WebSocket proxy，但前端实际连接 `ws://localhost:5173/api/v1/ws/events`，匹配的是 `/api` proxy 规则。`/ws` 规则永不被触发。

#### I6. 前端 TraceGroup/TraceSpan 类型存在双数据源

`types.ts` 中的类型匹配 serve-web.py 的 ClickHouse 响应格式。但 Go 后端 `listTraces` 返回不同字段名（`first_seen`→`earliest`、`duration_ms`→`duration`、`model`/`action`/`occurred_at`→`name`/`attributes`/`start_time`）。前端 API 客户端注释说明 traces API"经过 :8081"，但代码中无强制机制确保此路由。

### 7.3 死代码与未使用定义

| 位置 | 内容 | 状态 |
|------|------|------|
| `types.ts:110` | `SecurityEvent` 接口 | 定义但零引用 |
| `types.ts:57` | `SpanSummary` 接口 | 定义但零引用 |
| `DashboardPage.tsx:2` | `Activity` icon import | 导入但未使用 |
| `grpc/server.go:69-90` | `HeartbeatService` 结构体 | 已实现但从未实例化 |
| `grpc/server.go:50-55` | gRPC 21 methods | 生成的 stub 全未注册 |
| `vite.config.ts:14-17` | `/ws` proxy rule | 永不匹配 |
| `loader/src/main.rs:96-98` | perf reader 内层循环体 | 计算 available 但不处理 |
| `serve-web.py:630-636` | `_serve_traces_page()` | 独立 traces HTML 页面，与 React TracesPage 功能重复 |
| `Cargo.toml:27` | `tokio-test = "0.4"` | dev-dependency 未被任何测试使用 |

### 7.4 错误处理覆盖缺口

**前端页面错误状态（8/9 缺少）**：

| 页面 | 加载态 | 空态 | 错误态 |
|------|--------|------|--------|
| DashboardPage | ✅ | ✅ | ❌ fetch 失败 → 永久 spinner |
| AgentsPage | ✅ | ✅ | ❌ |
| AgentDetailPage | ✅ | ✅ | ❌ id 为 undefined 时永久 spinner |
| AuditLogPage | ✅ | ✅ | ❌ |
| AlertsPage | ✅ | ✅ | ❌ |
| PoliciesPage | ✅ | ✅ | ❌ |
| FamilyGroupsPage | ✅ | ✅ | ❌ |
| SecurityEventsPage | ✅ | ✅ | ❌ catch 块为空 |
| **TracesPage** | ✅ | ✅ | ✅ 红色错误卡片 + 错误信息 |

**WebSocket 健壮性**：
- 无 `onerror` 处理器——连接失败时静默重试，无用户可见反馈
- 组件卸载后 `setTimeout(connect)` 仍可能触发——潜在内存泄漏
- 无心跳/ping——服务端崩溃不发送 TCP close 时连接状态永远显示"已连接"

### 7.5 agent-runtime 架构问题

**Hermes stdout/stderr 管道无人读取**：[supervisor.rs:19-21](agent-runtime/src/supervisor.rs#L19-L21) 仅 `stdout(Stdio::piped())` 和 `stderr(Stdio::piped())`，但从不对管道执行读操作。若 Hermes 输出超过 OS 管道缓冲区（通常 64KB），子进程将阻塞。

**ProbeEvent 反序列化无验证**：[probe_manager.rs:188-192](agent-runtime/src/probe_manager.rs#L188-L192) 使用 `unsafe { ptr::read_unaligned(...) }` 从原始字节读取 564 字节结构体，仅检查长度≥size_of，但不验证对齐、魔数或版本号。eBPF 程序与用户态代码结构体布局漂移将导致静默内存破坏。

**无集成测试**：`agent-runtime/` 和 `ebpf-probes/` 仅有 19 个单元测试（3 个文件），无任何集成测试。`client.rs`、`probe_manager.rs`、`supervisor.rs`、`heartbeat.rs`、`event_upload.rs` 均无测试覆盖。

### 7.6 架构图（实际运行态）

```
                        ┌──────────────────────────┐
                        │     serve-web.py :8081    │
                        │  SPA + API proxy + Trace  │
                        │  + OPA evaluation (dup!)  │
                        └─────┬──────────┬─────────┘
                              │          │
              /api/v1/traces  │          │ /api/* (proxy)
              /family-groups- │          │ /api/v1/audit/events
              with-agents     │          │ (OPA intercept → forward)
                              │          │
                              ▼          ▼
┌──────────────┐    ┌──────────────────────────────┐    ┌──────────────┐
│  ClickHouse  │◄───│  management-server Go :8080   │───►│  OPA :8181   │
│ agentshield   │    │  22 HTTP routes (all work)    │    │  audit.rego  │
│ .spans       │    │  gRPC :9090 (0 handlers!)     │    │  authz.rego  │
└──────────────┘    │  SQLite data/agentshield.db    │    └──────────────┘
                    │  Risk Engine (rules+EMA)       │
                    │  WebSocket Hub (audit+alert)   │
                    └──────────┬───────────────────┘
                               │ POST /audit/events
                               │ POST /agents/heartbeat
                               │ POST /agents/register
                               ▼
                    ┌──────────────────────────────────┐
                    │  agent-runtime (Rust/Tokio)      │
                    │  ├─ eBPF: 4 tracepoints          │
                    │  │   (tid=0, uid=0, net_addr=∅)  │
                    │  ├─ EventBuffer (ring, cap 10k)  │
                    │  ├─ Heartbeat (sysinfo cpu/mem)  │
                    │  ├─ Supervisor (hermes)          │
                    │  │   stdout pipe → never read(!) │
                    │  ├─ PolicyCache (file-based)     │
                    │  └─ Demo mode fallback           │
                    └──────────────────────────────────┘

                    ┌──────────────────────────────────┐
                    │  React SPA (Vite :5173 dev)      │
                    │  Proxy: traces→:8081, /api→:8080  │
                    │  8 pages, 8/9 lack error states   │
                    │  WebSocket: no ping, leak on nav  │
                    └──────────────────────────────────┘
```

### 7.7 更新后的后续开发建议（按优先级）

#### P0 — 阻断性问题（立修）

1. **修复 gRPC**：实现 `ManagementServiceServer` 接口或移除 gRPC 启动代码
2. **修复 `engine_test.go`**：使 `checkThresholds` 测试签名与实际函数匹配
3. **修复 eBPF 数据捕获**：提取 tid（`bpf_get_current_pid_tgid()`）、uid（`bpf_get_current_uid_gid()`）、connect/bind sockaddr、execve argv
4. **编写 eBPF 构建脚本**：`build-ebpf.sh` 含 `cargo install bpf-linker` + `cargo build --target bpfel-unknown-none`
5. **消除 OPA 双重评估**：选择 serve-web.py 或 Go 后端中的一方执行 OPA 评估

#### P1 — 接口对齐（本周）

6. **统一 Trace API 格式**：serve-web.py 和 Go `listTraces` 二选一作为唯一数据源，删掉另一个实现
7. **修复 `healthz` 路径**：前端改 `BASE = ''` 调用 healthz，或 Go 注册 `/api/v1/healthz`
8. **修复 serve-web.py 字段名**：`name` → `display_name`，与 Go 模型对齐
9. **修复 `AlertsPage` 状态显示**：加 status→color 映射，区分 open/acknowledged/resolved/dismissed
10. **前端全页面添加错误状态**：8 个页面加 catch→setError 模式

#### P2 — 健壮性增强（下周）

11. **WebSocket 加 onerror + mounted ref + ping/pong**
12. **agent-runtime supervisor 消费 hermes stdout/stderr**（至少读到 /dev/null）
13. **ProbeEvent 反序列化加验证**（魔数或版本号）
14. **补充 agent-runtime 集成测试**：client、probe_manager、supervisor
15. **清理死代码**：删除 SecurityEvent/SpanSummary 类型、Activity import、`/ws` proxy rule、HeartbeatService、独立 traces HTML 页面

#### P3 — 功能完善

16. **PoliciesPage 加 Activate 按钮**
17. **ProbeEvent 字段 tid/uid 实际提取**
18. **Agent.labels 前端类型改 optional**
19. **PolicyBundle 前端类型补 policy_type + metadata 字段**
20. **去重 serve-web.py traces HTML 页面与 React TracesPage**

---

## 八、断点续跑快照机制 —— 详细设计

### 7.0 主流 Agent 框架快照机制调研

在设计之前，先梳理业界主流 Agent 框架的快照/回滚机制：

| 框架 | 快照粒度 | 技术方案 | 存储后端 | 回滚方式 |
|------|---------|---------|---------|---------|
| **LangGraph** | 每个 graph node (step) | 完整 State 序列化（JSON/Pickle） | MemorySaver / SqliteSaver / PostgresSaver | 加载 checkpoint，从该 step 继续执行 |
| **Claude Agent SDK** | 每个 tool-call 返回后 | 消息历史全量保存 | 会话级持久化（服务端） | 截断消息历史到 checkpoint 点 + 重新调用 |
| **CrewAI** | 每个 Task 完成后 | TaskOutput + AgentState 序列化 | JSON 文件 | 加载 TaskOutput 状态，跳过已完成 Task |
| **AutoGPT** | 每个 cycle（Think→Act→Observe） | AgentState（plan, memory, history）JSON 快照 | 本地文件 / Redis | 恢复 AgentState 并注入调整后的 plan |
| **Semantic Kernel** | 每个 Function 调用 | ChatHistory + Kernel state 序列化 | 内存 | 重放 ChatHistory 重建状态 |
| **OpenAI Swarm** | 每个 handoff / tool call | Context Variables + 消息历史 | 内存（无持久化） | 重置到断点 context |
| **Docker/CRIU** | 容器/进程级 | 进程内存 + 文件描述符 + 网络状态全量 dump | 磁盘镜像文件 | CRIU restore 重建完整进程 |

**关键发现：**

所有主流 Agent 框架均采用 **State Journal（状态日志）** 模式而非操作系统级 COW（写时复制）：
- Agent 的状态本质上是**文本数据**（消息历史 + 工具输出 + 工作记忆），序列化成本极低（< 1MB / checkpoint）
- OS 级 COW（`fork`/CRIU）需要完整进程内存快照（100MB-1GB+），对高频 checkpoint 而言太重
- 只需保存"Agent 在下一步行动前需要知道什么"，而非"进程此刻的每页内存"

### 7.1 核心设计：State Journal + Workspace Layering（状态日志 + 工作区分层）

AgentShield 采用 **双平面快照** 架构：

```
┌─────────────────────────────────────────────────┐
│                CheckpointManager                 │
│                                                  │
│  ┌──────────────────┐  ┌────────────────────┐   │
│  │  Conversation     │  │  Workspace Tracker │   │
│  │  Journal          │  │  (文件系统差异)     │   │
│  │  (消息历史序列化)  │  │                    │   │
│  └────────┬─────────┘  └────────┬───────────┘   │
│           │                      │               │
│           ▼                      ▼               │
│  ┌──────────────────┐  ┌────────────────────┐   │
│  │  Journal Entry   │  │  File Diff Store   │   │
│  │  {msg, tools,    │  │  {created, mod,    │   │
│  │   memory, usage} │  │   deleted, backup} │   │
│  └────────┬─────────┘  └────────┬───────────┘   │
│           │                      │               │
│           └──────────┬───────────┘               │
│                      ▼                           │
│           ┌──────────────────┐                   │
│           │  Recovery Engine │                   │
│           │  (回滚+重注入)    │                   │
│           └──────────────────┘                   │
└─────────────────────────────────────────────────┘
```

**平面 1 — Conversation Journal（对话日志）**

在 Agent 的每个 ReAct 循环边界（Tool Call 发出前），完整保存：
- **消息历史**（system + user + assistant + tool 消息的全量列表）
- **工具定义**（当前可用的 tools/functions schema）
- **工作记忆**（Agent 维护的 scratchpad / memory）
- **Token 用量**（累计 input/output tokens）
- **风险评分快照**（当前 EMA 分数）

格式：MessagePack 二进制（比 JSON 快 3-5 倍，体积小 30%）

**平面 2 — Workspace Layering（工作区分层）**

在每次 checkpoint 时，记录工作区文件变更：
```
checkpoint-N/
  ├── journal.msgpack       # 平面1: 对话状态
  ├── file_manifest.json    # {path: sha256} 当前文件清单
  └── clean_copies/         # 被修改/删除文件的备份副本
      └── output.txt.bak
```

文件变更追踪方式：**inotify 事件 + 定期哈希校验**（双重保障）

### 7.2 运行流程

```
Agent 运行时间线
─────────────────────────────────────────────────────►

Think → [Checkpoint] → Act → Observe → Think → [Checkpoint] → Act → Observe
  │         ▲                                          │         ▲
  │         │                                          │         │
  │    ✅ 保存状态                                     │    ✅ 保存状态
  │    ✅ 记录文件清单                                  │    ✅ 记录文件清单
  │                                                    │
  └── 正常执行 ────────────────────────────────────────┘
  

检测到异常时:
─────────────────────────────────────────────────────►

Think → [Checkpoint #3] → Act(被拦截!) → ⚠️ 异常触发
  │                              │
  │                              ▼
  │                    1. 停止 Agent 推理
  │                    2. 加载 Checkpoint #3
  │                    3. 回滚文件变更
  │                    4. 注入修复提示词
  │                    5. 恢复执行 ──→ 新的 Act (避开错误路径)
```

**Agent-Runtime 侧（Rust）：**

```
checkpoint_manager.rs
├── create() → CheckpointId
│   1. 从 Hermes stdin/stdout 流中提取当前消息历史
│   2. 序列化为 MessagePack
│   3. 扫描工作区文件 → 计算 SHA256 → manifest
│   4. 对修改过的文件，复制备份到 clean_copies/
│   5. 写入磁盘，返回 checkpoint_id
│
├── rollback(checkpoint_id) → Result<()>
│   1. 停止 Hermes 子进程
│   2. 读取 checkpoint journal
│   3. 对比当前文件与 manifest：
│      - 新增文件（manifest 中不存在）→ 删除
│      - 修改文件（hash 不匹配）→ 从 clean_copies/ 恢复
│      - 删除文件（manifest 中存在但当前不存在）→ 从 clean_copies/ 恢复
│   4. 注入 remediation prompt 到消息历史末尾
│   5. 用恢复后的状态重启 Hermes
│
├── list() → Vec<CheckpointMeta>
└── prune(max_count)  // 保留最近 N 个 checkpoint，删除旧的
```

**文件操作矩阵（回滚时）：**

| 状态 | 操作 |
|------|------|
| 文件存在于 checkpoint manifest，当前已删除 | 从 `clean_copies/` 恢复 |
| 文件存在于 checkpoint，当前已修改 (hash 不同) | 从 `clean_copies/` 覆盖 |
| 文件不存在于 checkpoint manifest，当前存在 | 删除（agent 新建的） |
| 文件存在于 checkpoint，当前未变 (hash 一致) | 无操作 |

### 7.3 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| 序列化格式 | **MessagePack** (`rmp-serde`) | 二进制高效，Rust/Python 原生支持 |
| 哈希算法 | **BLAKE3** | 比 SHA256 快 10x，Rust 原生实现 |
| 文件监控 | **inotify** (`inotify` crate) | Linux 内核原生，零开销事件流 |
| 存储布局 | **扁平文件** (`/var/lib/agentshield/checkpoints/{agent_id}/`) | 零依赖，Docker volume 友好 |
| Agent 通信 | **stdin/stdout 流 + Unix domain socket** | 与现有 Supervisor 架构一致 |
| Checkpoint 触发 | **Agent 主动调用 + Supervisor 启发式兜底** | 优先精确，兜底可靠 |

**为什么不用 COW / CRIU / Docker Snapshot：**

| 方案 | 快照大小 | 延迟 | 支持恢复 | 跨平台 |
|------|---------|------|---------|--------|
| Linux `fork` + COW | 进程内存全量 (100MB+) | ~1ms | ❌ fork 是单向的，无法恢复到 fork 点 | Linux only |
| CRIU checkpoint | 进程完整态 (100MB-1GB+) | 1-5s | ✅ | Linux only (需内核配置) |
| Docker `overlay2` snapshot | 容器层增量 (10-100MB) | 100-500ms | ✅ (需 Docker API) | 需 Docker |
| **State Journal (本方案)** | **文本状态 (10KB-1MB)** | **<1ms 写** | **✅** | **全平台** |

State Journal 的优势：
1. **粒度精准** —— 只保存 Agent 的"认知状态"，不保存进程运行时噪声
2. **可审计** —— checkpoint 是结构化可读数据，支持事后分析
3. **可迁移** —— 一个 agent 的 checkpoint 可用于恢复另一个 agent（状态可移植）
4. **轻量** —— 无需 root 权限、无需内核特性、无需 Docker daemon

### 7.4 Agent-Runtime 新增模块

```
agent-runtime/src/checkpoint/
├── mod.rs              # CheckpointManager 主结构体
├── journal.rs          # 消息历史 MessagePack 序列化
├── manifest.rs         # 文件清单 + BLAKE3 哈希
├── workspace.rs        # inotify 文件变更追踪
└── recovery.rs         # 回滚编排 + remediation prompt 生成
```

**新增配置环境变量：**

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AGENTSHIELD_CHECKPOINT_ENABLED` | `true` | 启用快照 |
| `AGENTSHIELD_CHECKPOINT_DIR` | `/var/lib/agentshield/checkpoints` | 快照存储目录 |
| `AGENTSHIELD_CHECKPOINT_MAX_COUNT` | `50` | 最大保留快照数 |
| `AGENTSHIELD_WORKSPACE_DIR` | `/workspace` | Agent 工作区路径 |
| `AGENTSHIELD_CHECKPOINT_INTERVAL_STEPS` | `1` | 每 N 个 ReAct step 做一次 checkpoint |

**新增心跳 `suggested_action`：**

| action | 参数 | 说明 |
|--------|------|------|
| `rollback_to` | `checkpoint_id: "ckpt-00003"` | 回滚到指定快照并重新注入提示词 |
| `create_checkpoint` | — | 立即创建快照 |

### 7.5 Recovery Prompt（修复提示词注入）

回滚后，在消息历史末尾自动注入 remediation prompt：

```
<system>
[AgentShield Security Intervention]
Your previous action was intercepted at step #{step_number}.
Reason: {risk_reason} (risk score: {risk_score}).
The system has been restored to the state before that action.

Safety guidance:
- Do NOT retry the same blocked action.
- Available safe resource paths are: {safe_paths}
- If you need access to a blocked resource, request it through proper channels.

Continue your task from here, avoiding the blocked approach.
</system>
```

### 7.6 部署架构

```
Docker Compose 中的 agent-runtime 容器
┌────────────────────────────────────────────┐
│  agent-runtime (Rust)                      │
│                                            │
│  CheckpointManager ◄── heartbeat           │
│       │                  (mgmt-server)     │
│       │                                    │
│       ├── checkpoint/  ◄── Docker volume   │
│       │   ckpt-0001/      agentshield_     │
│       │   ckpt-0002/      checkpoints:/... │
│       │   ...                              │
│       │                                    │
│       ├── supervisor     ◄──→ Hermes proc  │
│       │   (stdin/stdout)                   │
│       │                                    │
│       └── workspace_tracker ◄── inotify    │
│              /workspace (volume)           │
└────────────────────────────────────────────┘
```

**docker-compose 新增卷：**

```yaml
agent-runtime:
  volumes:
    - agentshield_checkpoints:/var/lib/agentshield/checkpoints
    - workspace_data:/workspace

volumes:
  agentshield_checkpoints:
  workspace_data:
```

### 7.7 测试方案

#### 单元测试（Rust `#[cfg(test)]`）

| 测试用例 | 覆盖 |
|---------|------|
| `test_journal_roundtrip` | MessagePack 序列化→反序列化一致性 |
| `test_manifest_compute` | 文件清单哈希计算正确性 |
| `test_rollback_file_restore` | 模拟文件增/改/删三类变更，验证回滚恢复 |
| `test_checkpoint_prune` | 超出 max_count 后旧快照被清除 |
| `test_checkpoint_create_rollback_cycle` | 创建快照 → 修改状态 → 回滚 → 验证完全恢复 |
| `test_recovery_prompt_injection` | 回滚后消息历史末尾正确注入修复提示词 |

#### 集成测试（Python 脚本 + Docker Compose）

```python
# tests/integration/test_checkpoint_rollback.py

def test_hermes_resume_after_rollback():
    """模拟 Hermes agent 执行一半被回滚后继续完成任务"""
    # 1. 启动 Hermes agent，执行 3 个 step（checkpoint 自动创建）
    # 2. 在第 4 个 step 模拟高危操作，触发 anomaly
    # 3. 验证 agent-runtime 回滚到 step 3 的 checkpoint
    # 4. 验证 workspace 文件恢复到 step 3 状态
    # 5. 验证 Hermes 收到 remediation prompt 后避开错误路径
    # 6. 验证任务最终成功完成（使用不同方法）

def test_token_savings_from_checkpoint():
    """验证断点续跑相比全量重跑节省的 Token"""
    # 全量重跑: 100 steps × 2K tokens = ~200K tokens
    # 断点续跑: (3 steps re-executed from checkpoint) × 2K = ~6K tokens
    # 节省率: ~97%

def test_concurrent_checkpoints():
    """并发多个 agent 各自的 checkpoint 互不干扰"""

def test_inotify_file_tracking():
    """文件变更事件被正确捕获并记录到 manifest"""
```

#### 混沌测试

| 场景 | 预期行为 |
|------|---------|
| Agent 在第 10 步执行 `delete /etc/passwd` | 被 eBPF 拦截 → 回滚到第 9 步 → 注入 remediation → 继续 |
| Agent 写入恶意脚本到 `/tmp/exploit.sh` | 被 OPA deny → 回滚 → 恶意文件被清理 |
| Agent 网络连接到内网敏感 IP | 被 eBPF connect 探针捕获 → 熔断 → 回滚 |
| Workspace 文件被外部进程修改 | inotify 感知 → 标记为"外部变更" → 不作为 agent 变更处理 |
| Checkpoint 磁盘空间满 | 自动 prune 最旧 checkpoint → 写入新 checkpoint |

#### 性能基准

| 指标 | 目标值 | 测量方法 |
|------|--------|---------|
| Checkpoint 创建延迟 | < 10ms | `Instant::now()` 前后差值 |
| 回滚延迟（100 文件 workspace） | < 200ms | 同上 |
| Checkpoint 磁盘占用 / step | < 1MB | `du -sh` 每个 checkpoint 目录 |
| Agent 正常执行吞吐影响 | < 3% | 有/无 checkpoint 吞吐对比 |

### 7.8 实施路线

| 阶段 | 内容 | 预计工时 |
|------|------|---------|
| **Phase 1** | `journal.rs` + `manifest.rs` — 核心序列化 + 文件清单 | 2d |
| **Phase 2** | `workspace.rs` + `recovery.rs` — 文件追踪 + 回滚编排 | 2d |
| **Phase 3** | `CheckpointManager` — 整合 + heartbeat 集成 | 1d |
| **Phase 4** | 单元测试 + Docker 集成测试 | 2d |
| **Phase 5** | 混沌测试 + 性能基准 + 文档 | 1d |
| **合计** | | **~8d** |
