# Stream 3: serve-web.py + 前端 API 层对齐修复

> **终端标识**: `stream-3-python-api`
> **组件**: `management-server/serve-web.py` (Python) + `management-server/web/src/api/` (TypeScript)
> **依赖**: 与 Stream 1 (Go) 协调 C5/C6；与 Stream 4 (UI) 协调 types.ts
> **预计工时**: 4-5h

---

## 任务清单

### Task 3.1 — 移除 serve-web.py 中的 OPA 双重评估 🔴 Critical

**文件**: [serve-web.py](management-server/serve-web.py:276-347)

**问题**: `_audit_events_with_opa()` 拦截 `POST /api/v1/audit/events`，先评估 OPA，再转发给 Go 后端。Go 后端的 `appendAuditEvents` 再次评估 OPA。每次批量事件上传 → OPA 评估两次 → 可能产生重复告警。

**决策**: OPA 评估由 Go 后端负责（Stream 1 保留 `evaluateOPA`）。serve-web.py 只做透明代理。

**修复**:
1. 修改 `do_POST()` (line 219-225)：`/api/v1/audit/events` 不再特殊处理，走通用 `_proxy()`
2. 将 `opa_evaluate()` 和 `opa_evaluate_audit_events()` 函数标记为 deprecated 注释，暂不删除（若 Go 侧需要参考逻辑）
3. `do_POST` 改为：

```python
def do_POST(self):
    parsed = urllib.parse.urlparse(self.path)
    if parsed.path == "/api/v1/spans":
        self._ingest_spans()
    else:
        self._proxy()
```

4. 确保 `_proxy()` 正确转发 POST body（它已经通过 `Content-Length` header 读取 body）

### Task 3.2 — Trace API 格式统一决策与清理 🟡 Medium

**文件**: [serve-web.py](management-server/serve-web.py:197-202), [router.go](management-server/internal/api/router.go:42-44)

**背景**: serve-web.py `_list_traces` 从 ClickHouse 查询（OTLP 兼容 span 格式），Go `listTraces` 从 SQLite 查询（简化格式）。前端类型匹配 serve-web.py 格式。

**决策**: serve-web.py 是 Trace API 的权威数据源（ClickHouse spans 表包含 prompt/completion，SQLite 不含）。Go 端 endpoints 应移除。

**修复**:
1. **serve-web.py 侧——不做修改**。`_list_traces`、`_get_trace_detail`、`_traces_by_agent`、`_family_groups_with_agents` 保持不变。
2. 在文件顶部注释中记录：Trace API 的唯一实现位置是 serve-web.py。
3. 检查 `_ingest_spans()` (lines 349-397) 的 `agentshield.spans` 表 schema 与前端 `TraceSpan` 类型一致

### Task 3.3 — 修复字段名映射：`name` → `display_name` 🟡 Medium

**文件**: [serve-web.py](management-server/serve-web.py:604, 607-608)

**问题**: `_family_groups_with_agents()` 使用 `g.get("name", gid)` 和 `a.get("name", a["id"])`，但 Go 模型返回的是 `display_name` 字段。当 Go 返回 `{"id": "g1", "display_name": "我的家庭"}` 时，serve-web.py 读取 `name` 字段得到 None，fallback 到 ID。

**修复**:

```python
# Line 604: 改为
"name": g.get("display_name", gid),

# Lines 607-608: 改为  
"name": a.get("display_name", a["id"]),
"hostname": a.get("hostname", ""),
```

同时更新 `FamilyGroupWithAgents` 和 `AgentInfo` 返回对象，增加 `display_name` 字段以保持向后兼容：

```python
groups_with_agents.append({
    "id": gid,
    "name": g.get("display_name", gid),       # 修复后
    "display_name": g.get("display_name", gid), # 新增
    ...
})
```

### Task 3.4 — 删除独立 Traces HTML 页面 🟡 Medium

**文件**: [serve-web.py](management-server/serve-web.py:630-636, 648-1024)

**问题**: serve-web.py 携带一份完整的 ~400 行独立 Traces Viewer HTML（`TRACES_HTML`），与 React `TracesPage` 组件功能完全重复。两者可能表现不同。

**修复**:
1. 删除 `_serve_traces_page()` 方法 (lines 630-636)
2. 删除 `TRACES_HTML` 常量 (lines 648-1024)
3. `do_GET` 中 `/traces` 路径改为重定向到 SPA：`self.send_response(302); self.send_header("Location", "/"); self.end_headers()`
4. 或者直接删除 `/traces` 路由处理，让它 fall through 到 SPA

### Task 3.5 — 清理前端 `types.ts` 死代码 🟢 Low

**文件**: [types.ts](management-server/web/src/api/types.ts)

**修复**:
1. 删除 `SecurityEvent` 接口 (lines 110-122 左右) —— `SecurityEventsPage` 使用 `AuditEvent` 类型
2. 删除 `SpanSummary` 接口 (line 57-60 左右) —— 无任何页面引用
3. 确认删除后运行 `npm run build --prefix management-server/web` 无 TypeScript 错误

### Task 3.6 — SecurityEventsPage 强类型属性访问 🟡 Medium

**文件**: [SecurityEventsPage.tsx](management-server/web/src/pages/SecurityEventsPage.tsx:171-175)

**问题**: `ev.attributes?.opa_risk_level` 等通过 `Record<string, string>` 访问属性——编译期无安全检查。

**修复**:
1. 在 `types.ts` 中扩展 `AuditEvent`，为 eBPF 事件已知属性添加可选的强类型字段：

```typescript
export interface AuditEvent {
  event_id: string;
  occurred_at: string;
  family_group_id: string;
  agent_id: string;
  resource_ref: string;
  action: string;
  risk_contribution: number;
  attributes: AuditEventAttributes; // 替换 Record<string, string>
}

export interface AuditEventAttributes {
  // eBPF provided
  comm?: string;
  pid?: string;
  uid?: string;
  tid?: string;
  // OPA injected (by Go backend)
  opa_allow?: string;            // "true" | "false"
  opa_risk_level?: string;       // "low" | "medium" | "high" | "critical"
  opa_deny_sensitive_path?: string;
  opa_deny_network?: string;
  opa_risky_write?: string;
  opa_matched_path?: string;
  // Trace/linkage
  trace_id?: string;
  // Other free-form
  [key: string]: string | undefined;
}
```

2. 更新 `SecurityEventsPage.tsx` 使用新的强类型属性访问
3. 运行 `npm run build` 确认无类型错误

### Task 3.7 — 检查 Vite Proxy 配置正确性 🟢 Low

**文件**: [vite.config.ts](management-server/web/vite.config.ts)

**现状**: proxy 规则顺序正确（具体路径在前，通用 `/api` 在后）。但 `/ws` 规则是死代码。

**修复**:
1. **不移除 `/ws` rule** —— 它属于 Stream 4
2. 添加注释说明每条规则的用途和依赖的服务
3. 确认 `family-groups-with-agents` 规则正确路由到 `:8081`

---

## 协作约束（CRITICAL — 修改前必读）

### 共享契约 #8：serve-web.py 的 API 代理行为

serve-web.py 是前端和 Go 后端之间的**透明代理**。修改其行为时遵循：

| 路径模式 | 处理方式 | 原因 |
|----------|---------|------|
| `/api/v1/traces*` | serve-web.py 直接处理（ClickHouse） | Go 后端无 ClickHouse 访问 |
| `/api/v1/family-groups-with-agents` | serve-web.py 处理（调用 Go API 聚合） | Go 后端无此聚合端点 |
| `/api/v1/spans` (POST) | serve-web.py 处理（写入 ClickHouse） | span 数据归属 ClickHouse |
| `/api/v1/audit/events` (POST) | **透明代理**到 Go 后端 | OPA 由 Go 负责 |
| `/api/*` 其他 | 透明代理到 Go 后端 | 通用 API |
| 其他路径 | 返回 SPA `index.html` | SPA 客户端路由 |

### 共享契约 #9：前端 TypeScript 类型 → Stream 4

`types.ts` 由 Stream 3 主责维护。Stream 4 若需修改类型（如补充页面所需字段），应提交请求给 Stream 3，由 Stream 3 统一修改。避免两个流同时编辑 `types.ts` 造成合并冲突。

### 共享契约 #10：Trace API 响应格式（权威规范）

serve-web.py `_list_traces` 的响应格式是前端 `TraceGroup`/`TraceSpan` 类型的**唯一数据源**。格式如下（不可破坏性修改）：

```json
{
  "traces": [{
    "trace_id": "string (hex)",
    "span_count": 1,
    "earliest": "2026-05-20 10:00:00.000",
    "latest": "2026-05-20 10:00:05.000",
    "spans": [{
      "span_id": "string (hex)",
      "trace_id": "string (hex)",
      "parent_id": "string (hex or empty)",
      "name": "string (e.g. 'openai.chat.completions.create')",
      "kind": 1,
      "start_time": "2026-05-20 10:00:00.000",
      "end_time": "2026-05-20 10:00:05.000",
      "duration": 5000,
      "status_code": 0,
      "attributes": {
        "gen_ai.request.model": "gpt-4o",
        "gen_ai.system": "openai",
        "gen_ai.operation.name": "chat",
        "langtrace.service.type": "llm",
        "langtrace.service.name": "openai"
      },
      "events": [{
        "name": "gen_ai.content.prompt",
        "timestamp": 1234567890,
        "attributes": {
          "gen_ai.prompt": "[...serialized messages JSON...]"
        }
      }]
    }]
  }],
  "total": 1
}
```

### 共享契约 #11：`/api/v1/family-groups-with-agents` 返回格式

此端点由 serve-web.py 聚合后返回，前端 TracesPage 侧边栏依赖：

```json
{
  "groups": [{
    "id": "string",
    "name": "string (= Go display_name, NOT Go id)",
    "display_name": "string (same as name, added for clarity)",
    "agent_count": 2,
    "agents": [{
      "id": "string",
      "name": "string (= Go display_name, NOT Go id)",
      "hostname": "string (from agent attributes, may be empty)",
      "status": "online | offline | unknown"
    }]
  }]
}
```

---

## 自我验证清单

- [ ] `python serve-web.py` 启动成功，无 import 或语法错误
- [ ] `curl -X POST localhost:8081/api/v1/audit/events -d '{...}'` 直接转发到 `:8080`，不被 OPA 拦截
- [ ] `curl localhost:8081/api/v1/family-groups-with-agents` 返回的 `name` 字段是 `display_name` 值而非 ID
- [ ] `curl localhost:8081/traces` 重定向到 `/` 或返回 SPA
- [ ] `curl localhost:8081/api/v1/traces?limit=5` 返回 ClickHouse span 格式
- [ ] `npm run build --prefix management-server/web` TypeScript 零错误
- [ ] 前端 build 产出包含 `TracesPage` 和 `SecurityEventsPage` 的 JS

---

## 禁止事项

- ❌ **禁止移除 serve-web.py 的 span ingestion** (`_ingest_spans`) —— SDK 依赖此端点写入 ClickHouse
- ❌ **禁止修改 ClickHouse `agentshield.spans` 表结构** —— 除非同时更新 SDK 和前端类型
- ❌ **禁止在 serve-web.py 中重新引入 OPA 评估** —— OPA 已归 Go 后端
- ❌ **禁止修改 `tsconfig.json` 或 `vite.config.ts` 的构建输出路径**（`outDir: 'dist'`）
- ❌ **禁止引入新的 Python 依赖** —— serve-web.py 当前仅依赖标准库 + requests
