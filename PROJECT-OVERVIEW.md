# AgentShield — AI Agent 监管中心

AgentShield 对 AI Agent 的全生命周期进行监管：资产注册、行为追踪、安全策略评估、风险评分、柔性处置。

## 架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                        Web UI (:8081)                           │
│   Dashboard │ Agents │ Tracing │ Alerts │ Policies │ Audit     │
└──────────────────────────┬──────────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                 management-server (:8080 / :9090)                │
│   Go HTTP API │ Risk Engine (EMA + ML) │ OPA Client │ gRPC      │
│   SQLite/Memory Store │ WebSocket Push │ Policy Distributor     │
└──────┬──────────┬──────────┬───────────┬────────────────────────┘
       │          │          │           │
┌──────▼──┐ ┌─────▼─────┐ ┌─▼────────┐ ┌▼──────────────┐
│  OPA    │ │ ClickHouse│ │ Bridge   │ │ agent-runtime  │
│ (:8181) │ │  (:8123)  │ │ (Python) │ │  (Rust, on VM) │
│ Rego    │ │ agentshield│ │ spans →  │ │ heartbeat +    │
│ policies│ │  .spans   │ │ events   │ │ event upload    │
└─────────┘ └───────────┘ └──────────┘ └────────────────┘
```

---

## 已完成模块

### 1. management-server（Go — 监管中心）

**HTTP API 路由** (`internal/api/router.go`)：20 个端点

| 分类 | 端点 | 说明 |
|------|------|------|
| Health | `GET /healthz` | 健康检查 |
| Agents | `POST /api/v1/agents/register` | 智能体注册 |
| | `GET /api/v1/agents` | 列表（支持 family_group_id、status 过滤） |
| | `GET /api/v1/agents/{id}` | 详情 |
| | `PUT /api/v1/agents/{id}/status` | 更新状态 |
| Audit | `POST /api/v1/audit/events` | 批量上报审计事件 |
| | `GET /api/v1/audit/events` | 分页查询（支持 agent_id、family_group_id 过滤） |
| Family Groups | `GET /api/v1/family-groups` | 列表 |
| | `POST /api/v1/family-groups` | 创建 |
| | `GET /api/v1/family-groups/{id}` | 详情 |
| | `PUT /api/v1/family-groups/{id}` | 更新 |
| | `DELETE /api/v1/family-groups/{id}` | 删除 |
| Policies | `GET /api/v1/policies/bundles` | 策略包列表 |
| | `POST /api/v1/policies/bundles` | 上传策略包 |
| | `PUT /api/v1/policies/bundles/{version}/activate` | 激活策略 |
| Alerts | `GET /api/v1/alerts` | 告警列表（支持 severity、status 过滤） |
| | `PUT /api/v1/alerts/{alertId}` | 更新告警状态 |
| Dashboard | `GET /api/v1/dashboard/stats` | 仪表盘统计 |
| WebSocket | `GET /api/v1/ws/events` | 实时推送审计事件 + 告警 |

**风险评估引擎** (`internal/risk/`)：
- 规则引擎：敏感路径检测、写操作检测、网络访问检测
- EMA 指数平滑评分（alpha=0.3）
- 阈值告警：medium (0.3)、high (0.6)、critical (0.8)
- 可选 ML 混合评分（权重从 0.1 渐进至 0.7）
- `ScoreCFG()` 控制流图评分已实现，等待接入

**存储** (`internal/store/`)：SQLite（默认） / Memory（开发模式）双后端；SQLite 支持 `ListAuditEventsFiltered`、`GetDashboardStats`、`ListRiskAlerts` 等完整查询。

**React 前端** (`web/src/`)：7 个页面 — DashboardPage、AgentsPage、AgentDetailPage、AuditLogPage、AlertsPage、PoliciesPage、FamilyGroupsPage。含 WebSocket 实时推送、StatusBadge/SeverityBadge 组件。

### 2. agent-runtime（Rust — 端侧常驻守护进程）

部署在虚拟机/物理机上，9 个模块全部完成：

| 模块 | 说明 |
|------|------|
| `main.rs` | 入口，初始化 Tokio runtime |
| `config.rs` | 环境变量配置（AGENTSHIELD_* 前缀） |
| `client.rs` | REST 客户端，上报事件/心跳到 management-server |
| `heartbeat.rs` | 定时心跳，上报 CPU/内存/探针状态（sysinfo 0.31） |
| `event_buffer.rs` | 内存事件缓冲池 |
| `event_upload.rs` | 批量上传审计事件 |
| `probe_manager.rs` | eBPF 探针管理（当前 demo 模式，Aya BPF 加载代码待激活） |
| `policy_cache.rs` | 本地策略缓存与查询 |
| `supervisor.rs` | 外部进程管理（start/stop/restart） |

### 3. Python 代理服务（serve-web.py + :8081）

独立于 management-server 的代理层，集成多项功能：

- **Web UI 服务**：React SPA 静态文件
- **API 代理**：`/api/*` → management-server :8080（含 null→[] 安全转换）
- **Trace 页面** (`/traces`)：独立 HTML/JS 页面，含家庭组 → 智能体 → Traces 分级浏览
  - `GET /api/v1/traces` — 全部 trace 列表
  - `GET /api/v1/traces/{trace_id}` — trace 详情（spans + prompt/completion 内容）
  - `GET /api/v1/traces/by-agent?agent_id=X` — 按智能体过滤
  - `GET /api/v1/family-groups-with-agents` — 树形结构（侧边栏用）
- **Span 接入**：`POST /api/v1/spans` — 接收 OTLP 兼容 JSON / span 数组 / 单 span
- **OPA 策略评估**：拦截 `POST /api/v1/audit/events`，调用 OPA 注入判决结果后再转发

### 4. ClickHouse 自主 Schema（agentshield.spans）

完全独立于 Langtrace 的 span 存储：

```sql
agentshield.spans (
    trace_id, span_id, parent_id, name, kind,
    start_time DateTime64(3), end_time DateTime64(3), duration Int64,
    status_code, status_message,
    attributes String, events String, resource_attributes String,
    agent_id String, family_group_id String, project_name String,
    ingested_at DateTime64(3)
) ENGINE = MergeTree()
ORDER BY (family_group_id, agent_id, start_time)
```

### 5. OPA 策略引擎（:8181）

两条 Rego 策略，实时评估每次审计事件：

- **`agentshield/authz`** — 准入控制：allow/deny、敏感路径列表、网络访问控制、速率限制、风险分级
- **`agentshield/audit`** — 审计事件专用：allow、deny_sensitive_path、deny_network、risky_write、risk_level（互斥规则）、matched_sensitive_path

判决结果自动注入到审计事件的 attributes，违规自动生成告警。

### 6. Bridge（langtrace_bridge.py）

轮询 ClickHouse `agentshield.spans` 表，将新 span 转换为审计事件推送至 management-server。已移除 Langtrace project_id 依赖，改为按 agent_id 过滤自己的表。

### 7. AgentShield Tracer（SDK）

`sqdk/python/agentshield_tracer.py` — 替代 `langtrace-python-sdk` 的轻量级 tracer：

- `init_tracer()` — 从环境变量初始化
- `trace_llm_call()` — context manager，手动追踪 LLM 调用
- `wrap_openai()` — 自动包装 OpenAI client，所有调用自动埋点
- Span 数据包含 prompt/completion 内容、token 用量、模型信息
- 直接 POST 到 AgentShield 的 `/api/v1/spans`

### 8. 部署与基础设施

- **Docker Compose** (`deployments/docker-compose.yml`)：7 个 profiles、8 个 service 定义
- **Dockerfiles**：management-server、agent-runtime、bridge、ml-pipeline、gateway
- **交叉编译**：`agent-runtime/build-linux.sh`（Docker 内 musl 静态编译）
- **部署脚本**：`agent-runtime/deploy-vm.sh`（scp + systemd）
- **CI**：`.github/workflows/` 下包含 CI 配置

---

## 部分完成 / 待完善

| 模块 | 当前状态 | 待完成 |
|------|----------|--------|
| **eBPF 探针** | Rust 框架完整，Aya BPF 加载代码被注释 | 激活真实 eBPF 挂载（syscall tracepoint / kprobe） |
| **gRPC 流式** | Proto 定义完整（双向 streaming），Go/Rust 端均用 REST | 实现 gRPC streaming 心跳 |
| **Policy Distributor** | Channel 推送机制已编码，从未被调用 | 串联心跳响应 → agent 拉取策略 |
| **Policy Cache (agent)** | `policy_cache.rs` 初始化但 `store/get` 未被调用 | 接入策略执行路径 |
| **Error Handler** | executor / subscriber / aggregator 完整，proto 定义就绪 | 部署为独立服务，接入告警触发流程 |
| **ML Pipeline** | FastAPI 框架 + 各模块就绪，checkpoint 未提交 | 训练模型 + 部署推理 |
| **Gateway** | auth / ratelimit / relay 就绪 | 完整部署 + Envoy 前置 |
| **Neo4j CFG 分析** | Schema 定义（cypher），`ScoreCFG()` 已实现 | 构建图数据 + 接入实时评估 |
| **PostgreSQL 后端** | Store 接口支持，Config 已留 DSN | 实现 PG store 适配器 |
| **Isolation 执行** | 心跳响应含 `suggested_action: isolate` | agent 端实现真正隔离（杀进程/停容器） |
| **内核加固** | SELinux `.te` 文件存在 | 完整 MAC 策略 + COW 快照 |

---

## 建议实施优先级

```
第一优先（安全闭环）：
  1. OPA 策略引擎接入 Go 管理端 [已通过 Python PEP 实现]
  2. 策略下发到 agent 端 [Policy Distributor + agent policy_cache 串联]
  3. agent 端策略执行 + 隔离响应

第二优先（检测能力）：
  4. 真实 eBPF 探针激活
  5. CFG 异常检测接入

第三优先（生产化）：
  6. gRPC 流式替换 REST 心跳
  7. PostgreSQL 替换 SQLite
  8. Error Handler 部署
```

---

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 API | Go（net/http、gorilla/websocket、modernc/sqlite、gRPC） |
| 端侧 Agent | Rust（Tokio、reqwest、sysinfo、Aya eBPF） |
| 策略引擎 | OPA / Rego |
| 数据存储 | SQLite / ClickHouse / 规划 PostgreSQL + Neo4j |
| 前端 | React + TypeScript |
| Span 接入 | OpenTelemetry 兼容 JSON（OTLP → AgentShield `/api/v1/spans`） |
| ML | Python（PyTorch + DGL + FastAPI） |
| 部署 | Docker Compose（7 profiles）+ systemd |
| 通信 | REST + WebSocket / 规划 gRPC streaming |
