# Stream 4: 前端 UI 健壮性修复

> **终端标识**: `stream-4-frontend-ui`
> **组件**: `management-server/web/src/` (React 18 + TypeScript + Vite)
> **依赖**: 轻微依赖 Stream 3 (types.ts 变更)。可先开工，types 变更由 Stream 3 完成后同步。
> **预计工时**: 4-5h

---

## 任务清单

### Task 4.1 — 8 个页面添加错误状态（批量修复）🟡 Medium

**现状**: 仅 `TracesPage` 有错误状态 UI。其余 8 个页面 fetch 失败后要么永久显示 spinner，要么空 catch 块静默吞咽。

**通用修复模式**（每个页面相同结构）:

```typescript
// 添加状态
const [error, setError] = useState<string | null>(null);

// fetch 函数中
const fetchXxx = useCallback(async () => {
  setLoading(true);
  setError(null);
  try {
    const data = await api.xxx();
    // ... set state
  } catch (e) {
    setError(e instanceof Error ? e.message : '加载失败');
  } finally {
    setLoading(false);
  }
}, [/* deps */]);

// render 中
if (error) {
  return (
    <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
      <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
      <p style={{ fontSize: 13 }}>{error}</p>
      <button onClick={fetchXxx} style={retryButtonStyle}>重试</button>
    </div>
  );
}
```

**涉及文件**（8 个，全部独立，可并行修改）:

| # | 页面文件 | 当前 catch 行为 | 修复 |
|---|---------|----------------|------|
| 4.1a | `pages/DashboardPage.tsx` | `console.error(e)` → 永久 spinner | 加 error state + retry button |
| 4.1b | `pages/AgentsPage.tsx` | `console.error(e)` → 空表 | 加 error state + retry button |
| 4.1c | `pages/AgentDetailPage.tsx` | `console.error(e)` → 永久 spinner | 加 error state + retry button |
| 4.1d | `pages/AuditLogPage.tsx` | `console.error(e)` → 空表 | 加 error state + retry button |
| 4.1e | `pages/AlertsPage.tsx` | `console.error(e)` → 空表 | 加 error state + retry button |
| 4.1f | `pages/PoliciesPage.tsx` | `console.error(e)` → 空表 | 加 error state + retry button |
| 4.1g | `pages/FamilyGroupsPage.tsx` | `console.error(e)` → 空表 | 加 error state + retry button |
| 4.1h | `pages/SecurityEventsPage.tsx` | 空 catch 块 `catch { }` → 静默 | 加 error state + retry button |

> **注意**: `SecurityEventsPage.tsx` 有两个 useEffect（初始加载 + 10s 轮询）。只需在初始加载时设置 error，轮询失败应静默（避免每 10s 弹错误覆盖有效数据）。

### Task 4.2 — 修复 `AlertsPage` 状态显示混淆 🟡 Medium

**文件**: [AlertsPage.tsx](management-server/web/src/pages/AlertsPage.tsx:73)

**问题**: `<SeverityBadge severity={a.status} />` 用告警严重性（`low`/`medium`/`high`/`critical`）的颜色映射来显示状态（`open`/`acknowledged`/`resolved`/`dismissed`）。所有状态值落入 default 分支，显示为同一蓝色——用户无法区分。

**修复**:
1. 在 `AlertsPage.tsx` 中添加 `statusBadge` 样式映射：

```typescript
const statusBadge: Record<string, React.CSSProperties> = {
  open: { background: '#fee2e2', color: '#dc2626' },
  acknowledged: { background: '#fef3c7', color: '#b45309' },
  resolved: { background: '#d1fae5', color: '#047857' },
  dismissed: { background: '#f1f5f9', color: '#64748b' },
};

const statusLabel: Record<string, string> = {
  open: '待处理',
  acknowledged: '已确认',
  resolved: '已解决',
  dismissed: '已忽略',
};
```

2. 替换第 73 行的 `<SeverityBadge severity={a.status} />` 为：

```tsx
<span style={{
  padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
  ...(statusBadge[a.status] || statusBadge.open),
}}>
  {statusLabel[a.status] || a.status}
</span>
```

3. 保留原来告警严重性列的 `<SeverityBadge severity={a.severity} />`（它用正确的值）。

### Task 4.3 — WebSocket 健壮性修复 🟡 Medium

**文件**: [WebSocketContext.tsx](management-server/web/src/context/WebSocketContext.tsx)

**问题 A — 无 `onerror` handler**：连接失败时静默重试，用户无感知。

**修复**: 在 `connect` 函数中 `ws.onclose` 旁边添加 `ws.onerror`：

```typescript
ws.onerror = () => {
  // onclose will fire after onerror, triggering reconnect.
  // Optionally log to console for debugging.
  console.debug('[ws] connection error, will retry...');
};
```

**问题 B — 卸载后重连泄漏**：组件卸载后 `setTimeout(connect, delay)` 仍可能执行。

**修复**: 在 `connect` 函数中加 mounted flag 检查：

```typescript
let mounted = true;

const connect = () => {
  if (!mounted) return;
  // ... existing logic ...
  ws.onclose = () => {
    if (!mounted) return;
    // ... existing reconnect logic ...
  };
};

// In cleanup:
return () => {
  mounted = false;
  wsRef.current?.close();
};
```

**问题 C — 无心跳检测 zombie 连接**：服务端崩溃未发 TCP close 时，`connected` 永远为 `true`。

**修复**: 在 WebSocket 连接建立后，添加 30s 心跳 ping/pong：

```typescript
// After ws.onopen:
const pingInterval = setInterval(() => {
  if (ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'ping' }));
  }
}, 30000);

ws.onclose = () => {
  clearInterval(pingInterval);
  // ... reconnect logic ...
};

ws.onmessage = (event) => {
  try {
    const msg = JSON.parse(event.data);
    if (msg.type === 'pong') return;
    // ... existing message routing ...
  } catch { /* ignore parse errors */ }
};
```

### Task 4.4 — 删除 `DashboardPage` 未使用的 `Activity` import 🟢 Low

**文件**: [DashboardPage.tsx](management-server/web/src/pages/DashboardPage.tsx:2)

**修复**: 将 `import { Bot, Activity, AlertTriangle, BarChart3, Wifi } from 'lucide-react';` 改为 `import { Bot, AlertTriangle, BarChart3, Wifi } from 'lucide-react';`

### Task 4.5 — 删除 `vite.config.ts` 死 `/ws` proxy 规则 🟢 Low

**文件**: [vite.config.ts](management-server/web/vite.config.ts:14-17)

**问题**: `/ws` proxy 规则匹配 `ws://localhost:8080`，但 WebSocket 实际路径是 `/api/v1/ws/events`，匹配的是 `/api` 规则。`/ws` 规则永不被触发。

**修复**: 删除以下行：

```typescript
// 删除这 4 行：
'/ws': {
  target: 'ws://localhost:8080',
  ws: true,
},
```

### Task 4.6 — `PoliciesPage` 添加 Activate 按钮 🟢 Low

**文件**: [PoliciesPage.tsx](management-server/web/src/pages/PoliciesPage.tsx)

**问题**: Go 后端有 `PUT /api/v1/policies/bundles/{version}/activate` 端点，但前端策略页面只有查看和创建，无法激活策略包。

**修复**:
1. 在 `api/client.ts` 中添加 `activatePolicyBundle` 方法：

```typescript
activatePolicyBundle: (version: string) =>
  request(`/policies/bundles/${encodeURIComponent(version)}/activate`, { method: 'PUT' }),
```

2. 在 `PoliciesPage` 的每个非活跃 bundle 行添加 "激活" 按钮，点击时调用 `api.activatePolicyBundle(version)` 后刷新列表。

---

## 协作约束（CRITICAL — 修改前必读）

### 共享契约 #12：错误状态 UI 规范

所有页面采用统一的错误状态组件样式，确保视觉一致性：

```typescript
// 共享样式（可在各页面复用）
const errorContainerStyle: React.CSSProperties = {
  padding: 40,
  textAlign: 'center',
  background: '#fef2f2',
  borderRadius: 8,
  color: '#dc2626',
};

const retryButtonStyle: React.CSSProperties = {
  marginTop: 12,
  padding: '8px 16px',
  borderRadius: 8,
  border: '1px solid #fecaca',
  background: '#fff',
  color: '#dc2626',
  cursor: 'pointer',
  fontSize: 13,
};
```

### 共享契约 #13：TypeScript 类型变更流程

`types.ts` 由 **Stream 3 主责维护**。若 Stream 4 在修复 page 时发现需要新增/修改类型：

1. 在 page 文件中先使用临时类型断言（`as any`）
2. 提交类型变更请求给 Stream 3（注明需要的字段和类型）
3. Stream 3 完成后，Stream 4 移除临时断言

避免两个流同时编辑 `types.ts`。

### 共享契约 #14：页面使用的 API 端点

Stream 4 的每个页面对应的后端 API。若 Stream 1 修改了这些端点的响应格式，必须及时同步：

| 页面 | API 端点 | 关键响应字段 |
|------|---------|-------------|
| DashboardPage | `GET /api/v1/dashboard/stats` | `total_agents, online_agents, total_events_24h, high_alerts_24h, recent_alerts` |
| AgentsPage | `GET /api/v1/agents` | `agents[].id, display_name, status, risk_score, family_group_id, last_heartbeat_at` |
| AgentDetailPage | `GET /api/v1/agents/{id}`, `GET /api/v1/audit/events?...` | Agent 详情 + 关联审计事件 |
| AuditLogPage | `GET /api/v1/audit/events?...` | `events[], total` |
| AlertsPage | `GET /api/v1/alerts?...`, `PUT /api/v1/alerts/{id}` | `alerts[].alert_id, severity, title, status, occurred_at` |
| PoliciesPage | `GET /api/v1/policies/bundles`, `POST /api/v1/policies/bundles` | `bundles[].version, digest, active, created_at` |
| FamilyGroupsPage | `GET /api/v1/family-groups`, `POST`, `PUT`, `DELETE` | `groups[].id, display_name` |
| SecurityEventsPage | `GET /api/v1/audit/events?...` | `events[].event_id, occurred_at, action, resource_ref, risk_contribution, attributes` |
| TracesPage | `GET /api/v1/traces`, `GET /api/v1/traces/by-agent`, `GET /api/v1/family-groups-with-agents` | 见 Stream 3 共享契约 #10/#11 |

### 共享契约 #15：WebSocket 事件类型

WebSocket `connected` 状态被 `App.tsx` 侧边栏消费（显示绿色 Wi-Fi / 红色断开）。修复 WebSocket 时确保：

- `connected` 状态从 `true` → `false` 的过渡不过在每次页面导航时闪烁
- `ws.send(JSON.stringify({type:'ping'}))` 的 `ping` 消息不触发 `subscribers` 回调

---

## 自我验证清单

- [ ] `npm run build --prefix management-server/web` TypeScript 零错误 + Vite 构建成功
- [ ] 每个页面：停止 Go 后端 → 刷新 → 应显示红色错误卡片（非永久 spinner）
- [ ] 每个页面：点"重试"按钮 → 启动 Go 后端 → 数据正常加载
- [ ] AlertsPage：4 种状态颜色不同（红/黄/绿/灰）
- [ ] WebSocket：停止 Go 后端 → 30s 内心跳超时 → 侧边栏显示红色"连接断开"
- [ ] WebSocket：重启 Go 后端 → 自动重连 → 侧边栏恢复绿色"实时连接"
- [ ] WebSocket：在 AlertsPage 和其他页面间切换 → 无 WebSocket 重复连接
- [ ] PoliciesPage：非活跃 bundle 行显示"激活"按钮，点击后 PUT 成功并刷新
- [ ] `vite.config.ts` 中无 `/ws` 规则

---

## 禁止事项

- ❌ **禁止引入新的 npm 依赖** —— 保持零依赖添加（使用现有 react-router-dom, lucide-react, recharts）
- ❌ **禁止修改页面路由 path** —— 路由变更影响 App.tsx 中 navItems 和 `<Route>` 定义
- ❌ **禁止修改 `api/client.ts` 中的 `BASE` 常量** —— 可能影响所有 API 调用
- ❌ **禁止在错误状态中使用 `alert()` 弹窗** —— 使用内联红色卡片
- ❌ **禁止删除任何页面的加载状态或空状态** —— 只添加错误状态，不删现有状态
