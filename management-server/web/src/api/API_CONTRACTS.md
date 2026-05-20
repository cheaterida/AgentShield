# 前端 API 参数与数据类型契约

> 自动生成于 Task 4.1 修复过程。列出全部 9 个页面使用的 API 端点、请求参数、响应数据类型。
> 后端（Go + serve-web.py）修改任何端点行为前，必须对照本文档确认前端兼容性。

---

## 公共约定

- **Base URL**: `/api/v1`（Vite proxy → Go `:8080`，Trace API 走 `:8081`）
- **字段命名**: JSON 使用 `display_name`（蛇形），不使用 `name`
- **错误格式**: `{ error: "message" }`，HTTP status >= 400
- **Enum 值大小写**: 全部小写（`online` / `open` / `low` / `critical` 等）

---

## 1. DashboardPage

| 项 | 值 |
|----|-----|
| **端点** | `GET /api/v1/dashboard/stats` |
| **查询参数** | `family_group_id?` (string, 可选) |
| **响应类型** | `DashboardStats` |
| **轮询** | 10s + WebSocket `audit_event` 推送触发 |

```typescript
interface DashboardStats {
  agent_count: number;             // 智能体总数
  online_agent_count: number;      // 在线数
  suspicious_agent_count: number;  // 可疑数
  event_rate_last_hour: number;    // 近1h 事件/分钟
  open_alert_count: number;        // 未解决告警数
  critical_alert_count: number;    // 严重告警数
  recent_alerts: RiskAlert[];      // 最近告警列表
}
```

---

## 2. AgentsPage

| 项 | 值 |
|----|-----|
| **端点** | `GET /api/v1/agents` |
| **查询参数** | `status?` (string: `online` / `offline` / `suspicious` / `degraded`) |
| **响应类型** | `{ agents: Agent[] }` |
| **轮询** | 15s |

```typescript
interface Agent {
  id: string;
  family_group_id: string;
  display_name: string;            // ← 合同 C：必须是 display_name
  labels: Record<string, string>;
  status: 'online' | 'offline' | 'suspicious' | 'degraded' | 'unknown';
  risk_score: number;              // 0.0 ~ 1.0
  last_heartbeat_at: string | null; // ISO 8601
  registered_at: string;           // ISO 8601
  updated_at: string;              // ISO 8601
}
```

---

## 3. AgentDetailPage

| 项 | 值 |
|----|-----|
| **端点 1** | `GET /api/v1/agents/{id}` → `Agent` |
| **端点 2** | `GET /api/v1/audit/events?agent_id={id}&limit=20` → `{ events: AuditEvent[], total: number }` |
| **轮询** | 10s（两者同时刷新） |

---

## 4. AuditLogPage

| 项 | 值 |
|----|-----|
| **端点** | `GET /api/v1/audit/events` |
| **查询参数** | `limit` (number, 默认 50), `action?` (string), `agent_id?` (string) |
| **响应类型** | `{ events: AuditEvent[], total: number }` |
| **轮询** | 10s |

```typescript
interface AuditEvent {
  event_id: string;                // UUID v4
  occurred_at: string;             // RFC3339Nano
  family_group_id: string;
  agent_id: string;
  resource_ref: string;            // 文件名或 syscall 名称
  action: string;                  // read | write | exec | network_connect | socket_create | ...
  attributes: Record<string, string>; // ← 合同 D：OPA 注入属性 + eBPF 属性
  risk_contribution: number;       // float64
}
```

### AuditEvent.attributes 已知 key

| Key | 来源 | 类型 | 说明 |
|-----|------|------|------|
| `comm` | eBPF | string | 进程名 (16 bytes) |
| `pid` | eBPF | string | 进程 PID (十进制数字字符串) |
| `uid` | eBPF | string | 用户 UID (十进制数字字符串) |
| `tid` | eBPF | string | 线程 TID (十进制数字字符串) |
| `opa_allow` | Go OPA | `"true"` / `"false"` | OPA 允许/拒绝 |
| `opa_risk_level` | Go OPA | `"low"` / `"medium"` / `"high"` / `"critical"` | 风险等级 |
| `opa_deny_sensitive_path` | Go OPA | `"true"` (可选) | 敏感路径拒绝 |
| `opa_deny_network` | Go OPA | `"true"` (可选) | 网络拒绝 |
| `opa_risky_write` | Go OPA | `"true"` (可选) | 风险写操作 |
| `opa_matched_path` | Go OPA | string (可选) | 匹配的路径规则 |
| `trace_id` | SDK | string (可选) | 关联的 trace |

---

## 5. AlertsPage

| 项 | 值 |
|----|-----|
| **端点 1** | `GET /api/v1/alerts?severity={sev}&status={st}` → `{ alerts: RiskAlert[], total: number }` |
| **端点 2** | `PUT /api/v1/alerts/{alert_id}` body: `{ status: string }` |
| **查询参数** | `severity?` (`low`/`medium`/`high`/`critical`), `status?` (`open`/`acknowledged`/`resolved`/`dismissed`) |
| **轮询** | 15s |

```typescript
interface RiskAlert {
  alert_id: string;
  family_group_id: string;
  agent_id: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  title: string;
  description: string;
  status: 'open' | 'acknowledged' | 'resolved' | 'dismissed';
  metadata: Record<string, string> | null;
  occurred_at: string;
  resolved_at: string | null;
  created_at: string;
}
```

> **注意**: `severity` 和 `status` 是不同字段。AlertsPage 表格有两列 —— 级别（severity）和状态（status），不可混淆。

---

## 6. PoliciesPage

| 项 | 值 |
|----|-----|
| **端点 1** | `GET /api/v1/policies/bundles` → `{ bundles: PolicyBundle[] }` |
| **端点 2** | `POST /api/v1/policies/bundles` body: `{ version, payload, digest }` |
| **规划中** | `PUT /api/v1/policies/bundles/{version}/activate` (Task 4.6) |

```typescript
interface PolicyBundle {
  version: string;
  digest: string;    // SHA256 hex
  active: boolean;
  created_at: string;
}
```

---

## 7. FamilyGroupsPage

| 项 | 值 |
|----|-----|
| **端点 1** | `GET /api/v1/family-groups` → `{ groups: FamilyGroup[] }` |
| **端点 2** | `POST /api/v1/family-groups` body: `{ id, display_name, labels }` |
| **端点 3** | `PUT /api/v1/family-groups/{id}` body: `{ display_name, labels }` |
| **端点 4** | `DELETE /api/v1/family-groups/{id}` |

```typescript
interface FamilyGroup {
  id: string;
  display_name: string;            // ← 合同 C
  member_principal_ids: string[];
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
}
```

---

## 8. SecurityEventsPage

| 项 | 值 |
|----|-----|
| **端点** | `GET /api/v1/audit/events?limit=50&action={filter}` |
| **响应类型** | `{ events: AuditEvent[], total: number }` (同 AuditEvent) |
| **轮询** | 10s（轮询失败静默，不覆盖已有数据） |

> 使用 `AuditEvent.attributes` 的 OPA 注入字段渲染安全判定列。字段清单见上表。

---

## 9. TracesPage

| 项 | 值 |
|----|-----|
| **端点 1** | `GET /api/v1/traces?limit=&agent_id=&family_group_id=&trace_id=` → `{ traces: TraceGroup[], total: number }` |
| **端点 2** | `GET /api/v1/traces/by-agent?agent_id={id}&limit={n}` → `{ traces: TraceGroup[] }` |
| **端点 3** | `GET /api/v1/family-groups-with-agents` → `{ groups: FamilyGroupWithAgents[] }` |
| **数据源** | serve-web.py :8081 (ClickHouse spans 表) |

```typescript
interface TraceGroup {
  trace_id: string;        // hex
  span_count: number;
  earliest: string;        // "2026-05-20 10:00:00.000"
  latest: string;
  spans: TraceSpan[];
}

interface TraceSpan {
  trace_id: string;
  span_id: string;
  parent_id: string;
  name: string;            // e.g. "openai.chat.completions.create"
  kind: number;
  start_time: string;
  end_time: string;
  duration: number;        // ms
  status_code: number;
  attributes: Record<string, string>;
  events: SpanEvent[];
  agent_id: string;
  family_group_id: string;
}

interface SpanEvent {
  name: string;            // e.g. "gen_ai.content.prompt"
  attributes: Record<string, string>;
}

interface FamilyGroupWithAgents {
  id: string;
  name: string;            // = Go display_name（由 serve-web.py 填充）
  agent_count: number;
  agents: AgentInfo[];
}

interface AgentInfo {
  id: string;
  name?: string;           // = Go display_name
  hostname?: string;
  status: string;
}
```

---

## 数据流架构速查

```
React SPA (:5173 dev / static dist)
  │
  ├── Vite Proxy
  │     ├── /api/v1/traces* ──────────► serve-web.py :8081 ──► ClickHouse spans
  │     ├── /api/v1/family-groups-with-agents ──► serve-web.py :8081 (聚合 Go API)
  │     └── /api/* ───────────────────► Go management-server :8080 ──► SQLite
  │
  └── WebSocket /api/v1/ws/events ────► Go management-server :8080 (Hub broadcast)
```

---

## 字段名对照（合同 C）

| Go 模型字段 | JSON key | 前端类型字段 |
|------------|----------|-------------|
| `Agent.DisplayName` | `display_name` | `Agent.display_name` |
| `FamilyGroup.DisplayName` | `display_name` | `FamilyGroup.display_name` |
| `FamilyGroupWithAgents.name` | `name` | (由 serve-web.py 从 display_name 填充) |
| `AgentInfo.name` | `name` | (由 serve-web.py 从 display_name 填充) |
