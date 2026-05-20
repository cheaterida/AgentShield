# Stream 1: Go 后端 Bug 修复

> **终端标识**: `stream-1-go-backend`
> **组件**: `management-server/internal/` (Go 1.25)
> **依赖**: 无外部流依赖，可立即开工
> **预计工时**: 4-6h

---

## 任务清单

### Task 1.1 — 修复 `engine_test.go` 编译错误 🔴 Critical

**文件**: [engine_test.go](management-server/internal/risk/engine_test.go:178)

**问题**: `checkThresholds` 调用传 3 个 `(string, string, float64)` 参数，实际函数签名是 `(ev *models.AuditEvent, ema float64) *models.RiskAlert`。测试期望返回 `[]models.RiskAlert` 切片，实际返回单指针。

**修复**:
1. 阅读 [engine.go](management-server/internal/risk/engine.go:185-214) 中 `checkThresholds` 的实际实现
2. 重写测试用例，构造 `models.AuditEvent` 而非三个独立参数
3. 将断言从 `[]models.RiskAlert` 改为 `*models.RiskAlert`
4. 运行 `go test -race -count=1 ./internal/risk/...` 验证通过

### Task 1.2 — gRPC：实现或移除 🔴 Critical

**文件**: [server.go](management-server/internal/grpc/server.go), [main.go](management-server/cmd/server/main.go:94-100)

**现状**: gRPC 服务器在 `:9090` 启动监听，但 `RegisterManagementServiceServer` 被注释掉，零个 handler 注册。`HeartbeatService` 已实现但从未实例化。

**决策**: **移除 gRPC 服务器启动代码**（理由：HTTP API 已覆盖全部 22 个端点；gRPC 从未工作过；保留死代码持续产生维护负担o

**修复**:
1. 从 `main.go` 删除 gRPC server 的创建和 goroutine 启动 (lines 94-100)
2. 从 `main.go` 删除 `grpc` 包 import
3. 从 `main.go` 删除 `grpcSrv.GracefulStop()` 调用 (line 114)
4. 删除 `internal/grpc/server.go` 文件
5. 从 `go.mod` 移除 `google.golang.org/grpc` 依赖（`go mod tidy`）
6. 将 `AGENTSHIELD_GRPC_ADDR` 从 config.go 标记为 deprecated

**备选方案（若未来需要 gRPC）**: 不删除文件，而是在 `server.go` 中嵌入 `UnimplementedManagementServiceServer`，逐方法委派给 HTTP handler 共享的 store/risk engine。此方案工时为 2-3 天。

### Task 1.3 — 修复 `healthz` 路径对齐 🟡 Medium

**文件**: [router.go](management-server/internal/api/router.go:30)

**问题**: Go 注册 `GET /healthz`，前端请求 `GET /api/v1/healthz` → 404。

**修复**: 在 router.go 中同时注册两个路径：

```go
r.mux.HandleFunc("GET /healthz", r.healthz)
r.mux.HandleFunc("GET /api/v1/healthz", r.healthz)
```

### Task 1.4 — Trace API 格式决策与清理 🟡 Medium

**文件**: [router.go](management-server/internal/api/router.go:509-583)

**问题**: Go `listTraces` 从 SQLite `audit_events` 表查询（action=`llm.chat`），返回格式与 serve-web.py ClickHouse 格式不兼容。前端只消费 serve-web.py 格式。Go 版本是死代码。

**修复**:
1. **保留路由注册**（Vite proxy fallback），但修改 handler 实现使其返回与 serve-web.py 兼容的格式
2. 或者：完全删除 Go 端 `listTraces`/`listSpans`/`ingestSpans` handler，因为这些端点已被 serve-web.py 接管
3. 推荐方案 2（删除），在 router.go 中移除 routes 42-44 行的 span/trace 路由注册
4. 同步删除 `internal/api/router.go` 中对应的 handler 函数

### Task 1.5 — 死代码清理 🟢 Low

- 删除 `internal/grpc/` 整个目录（与 Task 1.2 联动）
- 检查 `config.go` 中 `GRPCAddr` 字段：添加 `// Deprecated` 注释

---

## 协作约束（CRITICAL — 修改前必读）

### 共享契约 #1：API 响应格式

以下端点被其他流消费，**修改响应格式前必须通知所有流**：

| 端点 | 消费者 | 格式要求 |
|------|--------|---------|
| `GET /api/v1/agents` | Stream 3 (serve-web.py), Stream 4 (AgentsPage) | `{ agents: [{ id, display_name, family_group_id, status, labels?, ... }] }` |
| `GET /api/v1/family-groups` | Stream 3 (serve-web.py) | `{ groups: [{ id, display_name, ... }] }` |
| `POST /api/v1/audit/events` | Stream 2 (agent-runtime) | Request: `{ events: [{ event_id, occurred_at, family_group_id, agent_id, resource_ref, action, attributes, risk_contribution }] }` |
| `GET /api/v1/audit/events` | Stream 3, Stream 4 (AuditLogPage, SecurityEventsPage) | `{ events: AuditEvent[], total: number }` |
| `GET /api/v1/ws/events` | Stream 4 (WebSocketContext) | WebSocket upgrade |

### 共享契约 #2：OPA 评估归 Go 后端

`POST /api/v1/audit/events` handler (`appendAuditEvents`) 在 Go 侧调用 `evaluateOPA`。Stream 3 将移除 serve-web.py 中的 OPA 评估逻辑。**Go 侧的 OPA 注入属性必须保持不变**：

```go
// router.go evaluateOPA 注入的属性，前端 SecurityEventsPage 依赖：
attrs["opa_allow"]            // "true" / "false"
attrs["opa_risk_level"]       // "low" / "medium" / "high" / "critical"
attrs["opa_deny_sensitive_path"] // "true" 或省略
attrs["opa_deny_network"]     // "true" 或省略
attrs["opa_risky_write"]      // "true" 或省略
```

### 共享契约 #3：字段命名

Go 模型使用 `display_name`（蛇形），JSON tag 为 `"display_name"`。不要在 Go 侧引入 `name` 别名。

### 共享契约 #4：数据库 Schema

`migrations/001_init.sql` 是权威 schema。任何对 SQLite 表结构的修改必须通过新增迁移文件（`migrations/003_xxx.sql`），不可直接修改 001。

---

## 自我验证清单

- [ ] `go build ./...` 编译通过
- [ ] `go vet ./...` 无警告
- [ ] `go test -race -count=1 ./...` 全部通过
- [ ] `make build-management` 构建成功
- [ ] HTTP server 启动后 `curl localhost:8080/api/v1/healthz` 返回 200
- [ ] `curl localhost:8080/api/v1/agents` 返回正确 JSON
- [ ] gRPC `:9090` 端口不再监听（若选择移除）

---

## 禁止事项

- ❌ **禁止修改 `models.go` 结构体字段名或 JSON tag**（影响 Stream 2/3/4）
- ❌ **禁止修改 `store.go` 接口签名**（影响所有消费者）
- ❌ **禁止修改 WebSocket Hub 的 Broadcast 消息格式**（影响 Stream 4）
- ❌ **禁止引入新的 Go 依赖**（除非经所有流确认）
- ❌ **禁止删除 `serve-web.py` 依赖的端点**（`/api/v1/family-groups`, `/api/v1/agents`）
