# AgentShield

面向中小企业的 **Agent 智能管控与安全监管** 单体/多模块仓库。服务端承担家庭组编排、合法访问审计、策略下发与监管模型迭代；员工侧部署 Agent、eBPF 探针与内核安全底座，形成「网关 → 容器/运行时 → 本地安全校验 → 集中监管 → 分级处置」的闭环。

## 推荐技术栈

| 模块 | 技术选型 | 理由 |
|------|-----------|------|
| `ebpf-probes/` | **C (libbpf) + Rust (Aya)** | Aya 负责内存安全的用户态与探针一体交付；`libbpf-c/` 可并存 CO-RE/经典 BPF 资产。 |
| `agent-runtime/` | **Rust** | 与探针共享内存模型，端侧静默驻留、资源占用低。 |
| `management-server/` | **Go + gRPC** | 云原生生态成熟，适合高并发探针日志与指令下发。 |
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
  - 运行后：`GET /healthz`，`POST /api/v1/agents/register`，`GET /api/v1/agents`，`POST /api/v1/audit/events`，`GET /api/v1/audit/events`
- 其余模块（风险评估决策、柔性防御四级响应、双域权限映射、内核底座）按目录渐进接入。

## 构建提示（摘要）

- **Rust 工作区**：根目录 `Cargo.toml`，成员含 `agent-runtime`、`ebpf-probes/agentshield-ebpf`、`ebpf-probes/agentshield-loader`。eBPF 目标需 Linux + `bpf-linker`，参见 [Aya 文档](https://aya-rs.dev/book/start/)。
- **Go 模块**：`gateway`、`management-server`、`error-handler` 各自独立 `go.mod`，模块路径前缀 `agentshield.dev/agentshield/...`（可按实际仓库 URL 替换）。
- **Python**：在 `ml-pipeline/` 执行 `pip install -e ".[dev]"`。

## 推荐数据流（与实现对齐）

1. **请求链**：用户/Webhook → `gateway` → `agent-runtime`（容器内 Agent）。
2. **验证链**：Agent 动作 → `ebpf-probes` → `security-policy`（含 CFG）→ 本地管控组件上报 `management-server`。
3. **监管链**：端侧采集 → `management-server` 审计与态势 → `ml-pipeline` 更新模型 → 动态策略回灌。
4. **处置链**：安全事件 → `error-handler` 按等级回滚/疏导/熔断。

## 后续扩展占位

- **柔性疏导**、**CFG 控制流图**：已在 `error-handler/`、`security-policy/` 中预留职责边界，实现时避免与网关过滤逻辑耦合。

## 许可证

待定。
