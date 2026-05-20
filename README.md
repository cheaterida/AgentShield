# AgentShield

面向中小企业的 **Agent 智能管控与安全监管** 单体/多模块仓库。服务端承担家庭组编排、合法访问审计、策略下发与监管模型迭代；员工侧部署 Agent、eBPF 探针与内核安全底座，形成「网关 → 容器/运行时 → 本地安全校验 → 集中监管 → 分级处置」的闭环。

## 推荐技术栈

| 模块 | 技术选型 | 理由 |
|------|-----------|------|
| `ebpf-probes/` | **C (libbpf) + Rust (Aya)** | Aya 负责内存安全的用户态与探针一体交付；`libbpf-c/` 可并存 CO-RE/经典 BPF 资产。 |
| `agent-runtime/` | **Rust** | 与探针共享内存模型，端侧静默驻留、资源占用低。 |
| `management-server/` | **Go** | 云原生生态成熟，HTTP REST API 覆盖全部 22 个端点；gRPC 已于 2026-05-20 移除（proto 保留在 `shared/proto/`）。 |
| `gateway/` | **Go + Envoy/OpenResty** | 数据面与自定义 Webhook/脱敏逻辑解耦；Envoy 示例见 `gateway/envoy/envoy.yaml`。 |
| `security-policy/` | **OPA (Rego) + GraphDB** | OPA 做准入；Neo4j / RedisGraph 存 CFG，`cfg/graph_schema.cypher` 为占位。 |
| `ml-pipeline/` | **Python (PyTorch + FastAPI + DGL)** | 图学习与 HTTP 推理接口统一；见 `ml-pipeline/pyproject.toml`。 |
| `error-handler/` | **Go（Temporal 或自研状态机）** | 回滚/疏导/熔断长事务与工作流友好；`go.mod` 内备注 Temporal 依赖线。 |

## 模块索引

| 目录 | 职责 |
|------|------|
| `gateway/` | 统一入口：Webhook、负载均衡、入站过滤、出站控制、租户/家庭组路由 |
| `agent-runtime/` | 端侧运行时：Agent 进程监管、与 OpenClaw/容器编排对接、探针生命周期、策略本地缓存 |
| `ebpf-probes/` | `agentshield-ebpf`（Aya）、`agentshield-loader`、可选 `libbpf-c/` |
| `security-policy/` | OPA Rego（`policies/`）、CFG 图存储模式（`cfg/`） |
| `management-server/` | **AgentShield** 监管中心：家庭组与成员、调度视图、合法访问日志、未注册流量检测、策略与模型下发 |
| `error-handler/` | 分级处置：幻觉快速回滚、中危柔性疏导/权限引导、高危熔断 |
| `kernel-hardening/` | 内核安全底座：SELinux/MAC 配置、COW 快照与恢复脚本、不可绕过基线 |
| `ml-pipeline/` | 家庭组行为特征、监管辅助模型与推理 API |
| `shared/` | 跨模块契约：Protobuf/OpenAPI、公共类型、SDK 生成物 |
| `observability/` | 指标、日志规范、追踪约定（各模块实现对接） |
| `deployments/` | 容器镜像、Helm/Kustomize、Compose 等部署编排 |
| `scripts/` | 开发/CI/本地联调辅助脚本 |

## 当前开发进度（监管端首版）

- **`management-server`**：已实现 HTTP API，对应架构图中 **「智能体资产管理」**（注册/列表）与 **「实时感知建模」** 的数据入口（审计事件写入/查询，内存环形缓冲）。
  - 仅起监管：`docker compose -f deployments/docker-compose.yml --profile core up -d management-server`
  - 与网关同 profile：`docker compose -f deployments/docker-compose.yml --profile full build management-server`
  - 运行后：`GET /healthz` 或 `GET /api/v1/healthz`，`POST /api/v1/agents/register`，`GET /api/v1/agents`，`POST /api/v1/audit/events`，`GET /api/v1/audit/events`
- 其余模块（风险评估决策、柔性防御四级响应、双域权限映射、内核底座）按目录渐进接入。

## 风险评分引擎

management-server 内置规则 + EMA 混合评分引擎（`internal/risk/engine.go`），对审计事件实时打分并生成告警。

### 评分公式

```
EMA = α × event_score + (1 - α) × previous_EMA
```

默认 α = 0.3。事件分数由三条规则叠加（上限 1.0）：

| 规则 | 触发条件 | 分值 |
|------|---------|------|
| SensitivePathRule | 资源路径匹配 `/etc/*`、`/root/*`、`/proc/*`、`~/.ssh/*` 等敏感路径 | 0.5 |
| WriteActionRule | 动作为 `write` 或 `exec` | 0.2 |
| NetworkAccessRule | 动作为 `network_connect` 或 `socket_create` | 0.3 |

### 告警阈值

`checkThresholds` 根据 EMA 值生成四级告警：

| EMA 范围 | Severity | 说明 |
|-----------|----------|------|
| < 0.30 | — | 不触发告警，返回 nil |
| [0.30, 0.60) | medium | 中危 |
| [0.60, 0.80) | high | 高危 |
| ≥ 0.80 | critical | 严重 |

### RiskAlert 结构

```json
{
  "alert_id": "alert_<event_id>",
  "family_group_id": "fg-1",
  "agent_id": "agent-x",
  "severity": "medium|high|critical",
  "title": "Risk threshold exceeded for <agent_id>",
  "description": "EMA risk score X.XX on resource <resource_ref>",
  "status": "open",
  "occurred_at": "2026-05-20T10:00:00Z",
  "resolved_at": null,
  "created_at": "2026-05-20T10:00:00Z"
}
```

### API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/alerts` | 查询告警列表，支持 `?severity=&status=&family_group_id=&limit=&offset=` |
| PUT | `/api/v1/alerts/{alertId}` | 更新告警状态，body: `{"status": "acknowledged\|resolved\|dismissed"}` |

告警通过 WebSocket (`/api/v1/ws/events`) 实时推送：
- `risk_alert` — 新告警产生
- `alert_update` — 告警状态变更

### 使用示例

**查询告警：**

```bash
# 查看所有高危及以上未关闭告警
curl "http://localhost:8080/api/v1/alerts?severity=high&status=open"

# 响应
{
  "alerts": [{ "alert_id": "alert_evt-001", "severity": "high", ... }],
  "total": 1
}
```

**确认告警：**

```bash
curl -X PUT http://localhost:8080/api/v1/alerts/alert_evt-001 \
  -H "Content-Type: application/json" \
  -d '{"status": "acknowledged"}'
```

**提交审计事件（自动触发评分 + 告警生成 + WebSocket 推送）：**

```bash
# 读取敏感文件 → SensitivePathRule +0.5 → EMA = 0 × 0.3 + 0.5 × 0.3 = 0.15 → 不触发告警
curl -X POST http://localhost:8080/api/v1/audit/events \
  -H "Content-Type: application/json" \
  -d '{
    "events": [{
      "event_id": "evt-001",
      "occurred_at": "2026-05-20T10:00:00Z",
      "family_group_id": "fg-1",
      "agent_id": "agent-x",
      "resource_ref": "/etc/passwd",
      "action": "read",
      "risk_contribution": 0.0
    }]
  }'
```

**特殊边界示例：**

```bash
# 同时触发三条规则（写 /etc/shadow）
# SensitivePathRule(0.5) + WriteActionRule(0.2) = 0.7（上限 1.0）
# EMA = 0 + 0.3 × 0.7 = 0.21 → 不触发告警（< 0.30）

# 连续两次写敏感文件 → EMA 累积
# 第1次: EMA = 0.21
# 第2次: EMA = 0.21 + 0.3 × (0.7 - 0.21) = 0.357 → medium 告警触发

# 网络连接敏感路径 → 最高单次得分
# SensitivePathRule(0.5) + NetworkAccessRule(0.3) = 0.8
# EMA = 0 + 0.3 × 0.8 = 0.24 → 不触发告警
# 需持续异常行为 EMA 才会越过阈值
```

其中 OPA 策略引擎会对审计事件注入以下属性（`evaluateOPA`），影响二次评分：

| 属性 | 可选值 | 说明 |
|------|--------|------|
| `opa_allow` | `"true"` / `"false"` | OPA 准入判定 |
| `opa_risk_level` | `"low"` / `"medium"` / `"high"` / `"critical"` | OPA 风险等级 |
| `opa_deny_sensitive_path` | `"true"`（可选） | 敏感路径被 OPA 拒绝 |
| `opa_deny_network` | `"true"`（可选） | 网络访问被 OPA 拒绝 |
| `opa_risky_write` | `"true"`（可选） | 写操作被 OPA 标记 |

### OPA 评估归属

> **OPA 评估由 Go 后端（management-server :8080）全权负责。** serve-web.py 仅做透明代理，不参与 OPA 逻辑。此变更生效于 2026-05-20（Stream 3, Task 3.1）。

## Agent Heartbeat API

`POST /api/v1/agents/heartbeat` — agent-runtime 定期上报心跳，Go 后端返回处置建议。

### 请求

```json
{
  "agent_id": "agent-x",
  "cpu_percent": 12.5,
  "memory_bytes": 104857600,
  "active_probes": 4,
  "local_policy_version": "v1.2.3",
  "buffered_event_count": 42
}
```

### 响应

```json
{
  "acknowledged": true,
  "latest_policy_version": "v1.2.4",
  "suggested_action": "update_policy"
}
```

### suggested_action 枚举

| 值 | 触发条件 | agent-runtime 行为 |
|---|---------|-------------------|
| `ok` | EMA < 0.6 | 正常运行 |
| `update_policy` | 本地版本 < 最新版本 | 拉取新策略 |
| `restart_probe` | 探针异常计数 | 重启 eBPF 探针 |
| `isolate` | EMA ≥ 0.8 | 暂停 Agent 操作，等待人工介入 |

### 使用示例

```bash
curl -X POST http://localhost:8080/api/v1/agents/heartbeat \
  -H "Content-Type: application/json" \
  -d '{
    "agent_id": "agent-x",
    "cpu_percent": 12.5,
    "memory_bytes": 104857600,
    "active_probes": 4,
    "local_policy_version": "v1.0.0",
    "buffered_event_count": 0
  }'
```

## eBPF 探针事件字段说明

`agent-runtime` 通过 4 个内核 tracepoint 捕获 Agent 行为事件，上传至 `management-server`。每个 `ProbeEvent`（`#[repr(C)]`）包含以下字段：

| 字段 | 大小 | 来源 | 说明 |
|------|------|------|------|
| `pid` | u32 | `bpf_get_current_pid_tgid() >> 32` | 用户态进程 PID（内核 TGID） |
| `tid` | u32 | `bpf_get_current_pid_tgid()` 低 32 位 | 用户态线程 TID（内核 PID） |
| `uid` | u32 | `bpf_get_current_uid_gid()` 低 32 位 | 触发事件的用户 UID |
| `comm` | `[u8; 16]` | `bpf_get_current_comm()` | 进程名（最多 15 字符 + null） |
| `syscall` | `[u8; 16]` | 硬编码 | `openat` / `execve` / `connect` / `bind` |
| `filename` | `[u8; 256]` | `bpf_probe_read_user_str` | 目标文件路径或 sockaddr 地址 |
| `argv` | `[u8; 256]` | `bpf_probe_read_user_str` | execve 的首个参数（`argv[0]`），其余 tracepoint 为空 |
| `retval` | i64 | tracepoint ctx | syscall 返回值（enter probe 始终为 0） |

### 4 个 Tracepoint 与 Action 映射

| Tracepoint | 捕获内容 | 上报 action |
|------------|----------|-------------|
| `sys_enter_openat` | 打开文件路径 + flags | `read` / `write`（由用户态根据 flags 判定） |
| `sys_enter_execve` | 可执行文件路径 + `argv[0]` | `exec` |
| `sys_enter_connect` | 目标 sockaddr 地址（IP:Port） | `network_connect` |
| `sys_enter_bind` | 绑定 sockaddr 地址 | `socket_create` |

> **注意**: OPA 策略和 risk engine 依赖 action 字符串做规则匹配，**不可修改**。

### 典型使用场景

**场景 1 — 检测未授权进程执行**：`execve` → `action: "exec"`，`resource_ref` 为可执行文件路径，`argv[0]` 携带脚本参数。OPA 策略可匹配 `action == "exec"` + 敏感路径前缀。

**场景 2 — 追踪文件读写**：`openat` → 用户态 `probe_event_conv` 根据 flags 位判定 `action: "read"` 或 `"write"`，`resource_ref` 为文件路径。Risk engine 将 `resource_ref` 与敏感路径规则比对（如 `/etc/shadow`），命中则 SensitivePathRule 追加 0.5 风险分。

**场景 3 — 网络外连检测**：`connect` → `action: "network_connect"`，`resource_ref` 为目标 `IP:Port`。OPA 策略按 IP 白名单 / 内网段过滤，外连命中时注入 `opa_deny_network: "true"`。

**场景 4 — 多线程 Agent 中区分线程**：主线程与子线程 `pid` 相同但 `tid` 不同，通过 `attributes.tid` 定位到具体线程的行为事件。

### 上传事件 JSON 示例（agent-runtime → management-server）

```bash
curl -X POST http://localhost:8080/api/v1/audit/events \
  -H "Content-Type: application/json" \
  -d '{
    "events": [{
      "event_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "occurred_at": "2026-05-20T12:34:56.789+08:00",
      "family_group_id": "fg-prod-01",
      "agent_id": "agent-vm-u2204-01",
      "resource_ref": "/usr/bin/python3",
      "action": "exec",
      "attributes": {
        "comm": "python3",
        "pid": "18420",
        "uid": "1000",
        "tid": "18420"
      },
      "risk_contribution": 0.0
    }]
  }'
```

### OPA 评估归属

> **OPA 评估由 Go 后端（management-server :8080）全权负责。** serve-web.py 仅做透明代理，不参与 OPA 逻辑。此变更生效于 2026-05-20（Stream 3, Task 3.1）。

---

## serve-web.py API 代理行为

serve-web.py (`:8081`) 是前端 SPA 与后端服务之间的代理层。不同路径由不同后端处理：

| 路径模式 | 处理方式 | 后端 |
|----------|---------|------|
| `POST /api/v1/spans` | serve-web.py 直接写入 ClickHouse | ClickHouse `agentshield.spans` |
| `GET /api/v1/traces` | serve-web.py 直接查询 ClickHouse | ClickHouse `agentshield.spans` |
| `GET /api/v1/traces/<trace_id>` | serve-web.py 直接查询 ClickHouse | ClickHouse `agentshield.spans` |
| `GET /api/v1/traces/by-agent` | serve-web.py 直接查询 ClickHouse | ClickHouse `agentshield.spans` |
| `GET /api/v1/family-groups-with-agents` | serve-web.py 聚合（调 Go API） | Go :8080 → serve-web.py 组装 |
| `POST /api/v1/audit/events` | **透明代理** → Go | Go :8080（含 OPA 评估） |
| `/api/*` 其他 | 透明代理 → Go | Go :8080 |
| 其他路径 | 返回 SPA `index.html` | `management-server/web/dist/` |

### Span 摄入示例（绕过 Go，直接写 ClickHouse）

```bash
# OTLP 兼容格式
curl -X POST http://localhost:8081/api/v1/spans \
  -H "Content-Type: application/json" \
  -H "X-AgentShield-Agent-ID: agent-x" \
  -H "X-AgentShield-Family-Group-ID: fg-1" \
  -d '{
    "spans": [{
      "trace_id": "abc123def456",
      "span_id": "span-001",
      "parent_id": "",
      "name": "openai.chat.completions.create",
      "kind": 1,
      "start_time": "2026-05-20 10:00:00.000",
      "end_time": "2026-05-20 10:00:05.000",
      "duration": 5000,
      "status_code": 0,
      "attributes": {
        "gen_ai.request.model": "gpt-4o",
        "gen_ai.system": "openai"
      },
      "events": [{
        "name": "gen_ai.content.prompt",
        "timestamp": 1716192000000000000,
        "attributes": {
          "gen_ai.prompt": "[{\"role\":\"user\",\"content\":\"Hello\"}]"
        }
      }]
    }]
  }'

# 响应
{"accepted": 1}
```

### Trace 查询示例（serve-web.py 直接读 ClickHouse）

```bash
# 列出最近 traces
curl "http://localhost:8081/api/v1/traces?limit=5"

# 按 agent 筛选
curl "http://localhost:8081/api/v1/traces/by-agent?agent_id=agent-x&limit=10"

# 查看 trace 详情
curl "http://localhost:8081/api/v1/traces/abc123def456"
```

### Family Groups with Agents 聚合查询

```bash
curl "http://localhost:8081/api/v1/family-groups-with-agents"

# 响应格式
{
  "groups": [{
    "id": "fg-1",
    "name": "我的家庭组",          // Go display_name，非 Go id
    "display_name": "我的家庭组",  // 同 name，为前端 clarity 新增
    "agent_count": 2,
    "agents": [{
      "id": "agent-x",
      "name": "我的 Agent",       // Go display_name
      "hostname": "devbox",
      "status": "online"
    }]
  }]
}
```

### 审计事件提交（透明代理 → Go）

```bash
# serve-web.py 不拦截 OPA，直接转发到 Go :8080
curl -X POST http://localhost:8081/api/v1/audit/events \
  -H "Content-Type: application/json" \
  -d '{
    "events": [{
      "event_id": "evt-002",
      "occurred_at": "2026-05-20T10:00:00Z",
      "family_group_id": "fg-1",
      "agent_id": "agent-x",
      "resource_ref": "/etc/shadow",
      "action": "write",
      "risk_contribution": 0.5
    }]
  }'
# → Go :8080 → OPA 评估（Go 侧） → SQLite 写入 → WebSocket 推送
```

## 构建提示（摘要）

- **Rust 工作区**：根目录 `Cargo.toml`，成员含 `agent-runtime`、`ebpf-probes/agentshield-ebpf`、`ebpf-probes/agentshield-loader`。eBPF 目标需 Linux + `bpf-linker`，参见 [Aya 文档](https://aya-rs.dev/book/start/)。
- **Go 模块**：`gateway`、`management-server`、`error-handler` 各自独立 `go.mod`，模块路径前缀 `agentshield.dev/agentshield/...`（可按实际仓库 URL 替换）。
- **Python**：在 `ml-pipeline/` 执行 `pip install -e ".[dev]"`。

## 本地开发环境启动

以下命令用于 Windows 宿主机（Docker Desktop + Python 3），从零启动完整项目。

### 1. OPA 策略引擎

```powershell
docker run -d --name opa -p 8181:8181 -v "C:\Users\Acer\Desktop\AgentShield\security-policy\policies:/policies:ro" openpolicyagent/opa:latest run --server --addr=:8181 /policies
```

### 2. 数据目录 + 管理服务器

```powershell
docker volume create management_server_data
docker run -d --name management-server -p 8080:8080 -v management_server_data:/data --env-file deployments/mgmt.env agentshield/management-server:dev
```

> `deployments/mgmt.env` 中配置 SQLite 路径 `/data/agentshield.db`，Docker 重启后数据不丢失。

### 3. Web 控制台（8081）

```bash
cd /c/Users/Acer/Desktop/AgentShield/management-server && python serve-web.py 8081 &
```

> 代理 `/api/*` → management-server (8080)，提供 SPA 面板和 ClickHouse 链路查询。

### 4. 文件传输服务器（9999，向 VM 推送文件）

```powershell
docker run -d --name file-server -p 9999:9999 -v "C:\Users\Acer\Desktop\AgentShield\agent-runtime\bin:/files:ro" python:alpine python -m http.server 9999 --directory /files
```

> **必须用 PowerShell 启动**，MSYS2/bash 会自动转换路径导致 Docker 挂载失败。

### 5. 更新 tracer 并部署到 VM

```bash
# 复制到 file-server 挂载目录
cp -f C:/Users/Acer/Desktop/AgentShield/sdk/python/agentshield_tracer.py C:/Users/Acer/Desktop/AgentShield/agent-runtime/bin/agentshield_tracer.py

# VM 上执行
wget -O /usr/local/lib/agentshield/agentshield_tracer.py http://100.123.70.98:9999/agentshield_tracer.py
systemctl restart hermes-agent
```

### 验证

| 端口 | 服务 | 验证命令 |
|------|------|----------|
| 8080 | management-server | `curl http://localhost:8080/healthz` 或 `curl http://localhost:8080/api/v1/healthz` |
| 8081 | Web 控制台 | `curl -o /dev/null -w "%{http_code}" http://localhost:8081/` |
| 8181 | OPA | `curl -o /dev/null -w "%{http_code}" http://localhost:8181/` |
| 9999 | file-server | `curl -o /dev/null -w "%{http_code}" http://localhost:9999/agentshield_tracer.py` |

### 一键全部停用

```bash
docker rm -f management-server opa file-server 2>/dev/null; pkill -f "serve-web.py"
```

## 推荐数据流（与实现对齐）

1. **请求链**：用户/Webhook → `gateway` → `agent-runtime`（容器内 Agent）。
2. **验证链**：Agent 动作 → `ebpf-probes` → `security-policy`（含 CFG）→ 本地管控组件上报 `management-server`。
3. **监管链**：端侧采集 → `management-server` 审计与态势 → `ml-pipeline` 更新模型 → 动态策略回灌。
4. **处置链**：安全事件 → `error-handler` 按等级回滚/疏导/熔断。

## 后续扩展占位

- **柔性疏导**、**CFG 控制流图**：已在 `error-handler/`、`security-policy/` 中预留职责边界，实现时避免与网关过滤逻辑耦合。

## 许可证

待定。
