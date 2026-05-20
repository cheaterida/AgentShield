# Token 算力配额管理 —— 详细设计

## 0. 业界 API 中转站配额管理架构调研

### 0.1 典型开源方案对比

| 项目 | 语言 | 架构模式 | 配额粒度 | 计价引擎 | 前端 |
|------|------|---------|---------|---------|------|
| **cc-switch** (`farion1231`) | Python | 反向代理中间件 | 按 API Key | 模型定价表 | Vue 管理面板 |
| **one-api** (`songquanpeng`) | Go | 反向代理 + 渠道管理 | 按用户组 / 按 Key | 模型定价表 + 倍率 | React 仪表盘 |
| **LiteLLM** (BerriAI) | Python | 统一 SDK / Proxy | 按 Team / User / Key | 模型定价 + 自定义 | Admin UI |
| **Helicone** | TypeScript | 网关代理 | 按 Workspace | 模型定价 | React Dashboard |
| **Portkey** | TypeScript/Go | 网关代理 | 按 Workspace / Virtual Key | 多模型定价 | React Dashboard |

### 0.2 核心架构模式对比

**模式 A：反向代理拦截（cc-switch / one-api 模式）**

```
Client → [API Relay] → Upstream LLM (OpenAI / DeepSeek / ...)
              │
              ├── 1. 验证 API Key
              ├── 2. 检查配额（请求前）
              ├── 3. 转发请求
              ├── 4. 从响应中提取 usage.token 数据
              ├── 5. 扣减配额（请求后）
              └── 6. 超额时返回 429 / 自定义错误
```

优点：零侵入，客户端无需改动
缺点：多一跳时延（~10-50ms），需要维护反向代理基础设施

**模式 B：SDK 旁路上报（AgentShield 现有模式）**

```
Client (SDK wrapped) → Upstream LLM
     │                      │
     │ token usage          │ response
     ▼                      ▼
  Management Server ←─ Span/Audit Event
       │
       ├── 异步累积用量
       └── 下一轮心跳时下发限流指令
```

优点：不增加推理路径时延，与现有 SDK 完全复用
缺点：非实时（心跳间隔延迟 ~10s），无法"前置拦截"

**模式 C：混合模式（推荐）**

```
Client (SDK wrapped) → Upstream LLM
     │                      │
     │ ① 实时 quota 预检    │ response
     │    (轻量 Redis 查询)  │
     ▼                      ▼
  Quota Service ←──── Span(token usage)
  (Redis + SQLite)
       │
       ├── ② quota 超标 → 429 拒绝
       └── ③ 心跳同步 → 更新本地缓存
```

优点：兼顾实时性与低侵入
缺点：需 SDK 侧增加一次轻量查询

### 0.3 关键设计决策

| 决策点 | cc-switch / one-api 做法 | AgentShield 适配方案 |
|--------|-------------------------|-------------------|
| 流量路径 | 代理拦截（inline） | SDK 旁路 + 心跳反馈（out-of-band） |
| 配额存储 | MySQL / PostgreSQL | SQLite + Redis 缓存 |
| Token 解析 | HTTP Response body `usage` 字段 | SDK `span.set_usage()` 已捕获 |
| 超额处理 | 返回 HTTP 429 | 心跳下发 `quota_exceeded` → Agent 自限流 |
| 定价模型 | 硬编码模型定价表 | 可配置 `model_prices` 表 |
| 前端 | 管理面板 | 扩展 React Dashboard |

---

## 1. 核心设计

### 1.1 整体架构

```
┌─────────────────────────────────────────────────────┐
│                  TokenQuotaManager                    │
│                                                      │
│  ┌──────────────────┐  ┌─────────────────────────┐  │
│  │  Usage Collector  │  │  Quota Enforcer          │  │
│  │  (用量采集)        │  │  (配额裁决)              │  │
│  │                  │  │                          │  │
│  │  从 Span 提取     │  │  检查 agent/family_group │  │
│  │  token 字段       │  │  配额 → 超标时：        │  │
│  │  → 写入 usage_log │  │  · 更新 agent 状态      │  │
│  └────────┬─────────┘  │  · 生成告警              │  │
│           │             │  · 心跳下发限流指令      │  │
│           ▼             └────────────┬────────────┘  │
│  ┌──────────────────┐               │               │
│  │  Usage Aggregator │               │               │
│  │  (用量聚合)        │               │               │
│  │                  │               │               │
│  │  定时任务：        │               │               │
│  │  · 汇总 daily 用量 │               │               │
│  │  · 汇总 monthly   │               │               │
│  │  · 汇总 per-model │               │               │
│  └────────┬─────────┘               │               │
│           │                          │               │
│           └──────────┬───────────────┘               │
│                      ▼                               │
│           ┌──────────────────┐                       │
│           │  Cost Calculator │                       │
│           │  (成本换算)       │                       │
│           │  tokens × 模型定价│                       │
│           └──────────────────┘                       │
└─────────────────────────────────────────────────────┘
```

### 1.2 数据模型

**新增 SQLite 表：**

```sql
-- 001_add_token_management.sql

-- 配额定义表：每个 Agent 或 FamilyGroup 的配额上限
CREATE TABLE IF NOT EXISTS token_quotas (
    quota_id        TEXT PRIMARY KEY,
    target_type     TEXT NOT NULL,       -- 'agent' | 'family_group'
    target_id       TEXT NOT NULL,       -- agent_id 或 family_group_id
    quota_name      TEXT NOT NULL DEFAULT 'default',
    -- 限额（-1 表示无限）
    daily_limit     INTEGER NOT NULL DEFAULT -1,
    weekly_limit    INTEGER NOT NULL DEFAULT -1,
    monthly_limit   INTEGER NOT NULL DEFAULT -1,
    total_limit     INTEGER NOT NULL DEFAULT -1,
    -- 单次请求限额
    per_request_limit INTEGER NOT NULL DEFAULT -1,
    -- 并发限制
    max_concurrency   INTEGER NOT NULL DEFAULT -1,  -- -1 不限制
    -- 告警阈值（0.0-1.0）
    warn_threshold    REAL NOT NULL DEFAULT 0.8,     -- 80% 时告警
    block_threshold   REAL NOT NULL DEFAULT 1.0,     -- 100% 时阻断
    -- 优先级（低优先级 Agent 在高负载时先被限流）
    priority          INTEGER NOT NULL DEFAULT 5,     -- 1-10, 10 最高
    active            INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Token 用量日志表：记录每次 LLM 调用的 Token 消耗
CREATE TABLE IF NOT EXISTS token_usage_logs (
    log_id          TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    family_group_id TEXT NOT NULL,
    span_id         TEXT NOT NULL,
    trace_id        TEXT NOT NULL DEFAULT '',
    -- 模型信息
    model_name      TEXT NOT NULL,
    provider        TEXT NOT NULL DEFAULT '',   -- 'openai' | 'deepseek' | 'anthropic'
    -- Token 细分
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    -- 缓存命中 Token（减少实际消耗）
    cache_read_tokens    INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens   INTEGER NOT NULL DEFAULT 0,
    -- 费用（单位：毫分，即万分之美元，避免浮点精度问题）
    cost_millicents INTEGER NOT NULL DEFAULT 0,
    -- 配额检查结果
    quota_status    TEXT NOT NULL DEFAULT 'ok',  -- ok | warned | blocked
    occurred_at     TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_usage_logs_agent ON token_usage_logs(agent_id, occurred_at);
CREATE INDEX idx_usage_logs_fg ON token_usage_logs(family_group_id, occurred_at);

-- 聚合用量表：预聚合数据加速查询
CREATE TABLE IF NOT EXISTS token_usage_summary (
    summary_id      TEXT PRIMARY KEY,  -- '{target_type}:{target_id}:{period}:{date_key}'
    target_type     TEXT NOT NULL,     -- 'agent' | 'family_group'
    target_id       TEXT NOT NULL,
    period          TEXT NOT NULL,     -- 'daily' | 'weekly' | 'monthly' | 'total'
    date_key        TEXT NOT NULL,     -- '2026-05-19' | '2026-W21' | '2026-05' | 'total'
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens    INTEGER NOT NULL DEFAULT 0,
    request_count   INTEGER NOT NULL DEFAULT 0,
    cost_millicents INTEGER NOT NULL DEFAULT 0,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX idx_usage_summary_unique ON token_usage_summary(target_type, target_id, period, date_key);

-- 模型定价表
CREATE TABLE IF NOT EXISTS model_prices (
    model_id        TEXT PRIMARY KEY,
    provider        TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    -- 价格（毫分 / 1M tokens）
    input_price_millicents  INTEGER NOT NULL DEFAULT 0,
    output_price_millicents INTEGER NOT NULL DEFAULT 0,
    cache_read_price_millicents  INTEGER NOT NULL DEFAULT 0,
    active          INTEGER NOT NULL DEFAULT 1,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 预置常用模型定价
INSERT INTO model_prices (model_id, provider, display_name, input_price_millicents, output_price_millicents) VALUES
    ('gpt-4o',              'openai',    'GPT-4o',               250000, 1000000),
    ('gpt-4o-mini',         'openai',    'GPT-4o Mini',           15000,   60000),
    ('deepseek-chat',       'deepseek',  'DeepSeek-V3',           27000,  110000),
    ('deepseek-reasoner',   'deepseek',  'DeepSeek-R1',           55000,  219000),
    ('claude-opus-4-7',     'anthropic', 'Claude Opus 4.7',     1500000, 7500000),
    ('claude-sonnet-4-6',   'anthropic', 'Claude Sonnet 4.6',    300000, 1500000),
    ('claude-haiku-4-5',    'anthropic', 'Claude Haiku 4.5',      80000,  400000),
    ('qwen-turbo',          'alibaba',   'Qwen Turbo',             3000,   60000)
ON CONFLICT (model_id) DO NOTHING;
```

### 1.3 Go 模型层扩展

```go
// internal/models/token_quota.go

type TokenQuota struct {
    QuotaID          string  `json:"quota_id"`
    TargetType       string  `json:"target_type"`    // agent | family_group
    TargetID         string  `json:"target_id"`
    QuotaName        string  `json:"quota_name"`
    DailyLimit       int64   `json:"daily_limit"`
    WeeklyLimit      int64   `json:"weekly_limit"`
    MonthlyLimit     int64   `json:"monthly_limit"`
    TotalLimit       int64   `json:"total_limit"`
    PerRequestLimit  int64   `json:"per_request_limit"`
    MaxConcurrency   int64   `json:"max_concurrency"`
    WarnThreshold    float64 `json:"warn_threshold"`
    BlockThreshold   float64 `json:"block_threshold"`
    Priority         int     `json:"priority"`
    Active           bool    `json:"active"`
    // 运行时计算字段（不持久化）
    DailyUsed        int64   `json:"daily_used,omitempty"`
    MonthlyUsed      int64   `json:"monthly_used,omitempty"`
}

type TokenUsageLog struct {
    LogID             string `json:"log_id"`
    AgentID           string `json:"agent_id"`
    FamilyGroupID     string `json:"family_group_id"`
    SpanID            string `json:"span_id"`
    ModelName         string `json:"model_name"`
    Provider          string `json:"provider"`
    InputTokens       int64  `json:"input_tokens"`
    OutputTokens      int64  `json:"output_tokens"`
    TotalTokens       int64  `json:"total_tokens"`
    CacheReadTokens   int64  `json:"cache_read_tokens"`
    CacheWriteTokens  int64  `json:"cache_write_tokens"`
    CostMillicents    int64  `json:"cost_millicents"`
    QuotaStatus       string `json:"quota_status"`
    OccurredAt        string `json:"occurred_at"`
}

type TokenUsageSummary struct {
    TargetType    string `json:"target_type"`
    TargetID      string `json:"target_id"`
    Period        string `json:"period"`    // daily | weekly | monthly | total
    DateKey       string `json:"date_key"`
    InputTokens   int64  `json:"input_tokens"`
    OutputTokens  int64  `json:"output_tokens"`
    TotalTokens   int64  `json:"total_tokens"`
    RequestCount  int64  `json:"request_count"`
    CostMillicents int64 `json:"cost_millicents"`
}

type ModelPrice struct {
    ModelID                  string `json:"model_id"`
    Provider                 string `json:"provider"`
    DisplayName              string `json:"display_name"`
    InputPriceMillicents     int64  `json:"input_price_millicents"`
    OutputPriceMillicents    int64  `json:"output_price_millicents"`
    CacheReadPriceMillicents int64  `json:"cache_read_price_millicents"`
}
```

### 1.4 TokenQuotaManager 核心逻辑

```
token_quota_manager.go
│
├── RecordUsage(span) → QuotaResult
│   1. 从 span attributes 提取 token 字段:
│      gen_ai.usage.input_tokens, gen_ai.usage.output_tokens
│      gen_ai.response.model → 查定价表
│   2. 写入 token_usage_logs
│   3. 计算成本: cost = input × input_price + output × output_price
│   4. 更新 token_usage_summary (daily/weekly/monthly/total)
│   5. 检查配额
│   6. 返回 ok | warned | blocked
│
├── CheckQuota(agentID, period) → QuotaStatus
│   1. 查 agent 自己的配额 → 若无则查 family_group 的配额
│   2. 查当前周期已用量
│   3. 对比限额:
│      · used/limit > block_threshold → blocked
│      · used/limit > warn_threshold → warned
│   4. 返回状态 + 已用量/限额比
│
├── GetAgentUsage(agentID) → UsageReport
│   daily + weekly + monthly + total 四维用量汇总
│
├── ListQuotas(filters) → []TokenQuota
├── CreateQuota(req) → TokenQuota
├── UpdateQuota(id, req) → TokenQuota
├── DeleteQuota(id)
│
└── AggregateUsages()  // 定时任务，从 logs 聚合到 summary
```

---

## 2. 配额裁决流程

### 2.1 实时路径（推荐 v1.1 实现）

```
Agent LLM 调用
      │
      ▼
SDK wrap_openai()
      │
      ├── ① 本地缓存检查 quota（从 agent-runtime 同步）
      │      ├── ok → 继续
      │      └── blocked → 返回自定义错误，不调用 LLM
      │
      ├── ② 调用 LLM
      │
      └── ③ span.set_usage() → flush to management-server
              │
              ▼
         TokenQuotaManager.RecordUsage()
              │
              ├── 超标 → 更新 agent.risk_score += 0.1
              │         生成 RiskAlert (severity: "low")
              │         下一次心跳返回 suggested_action: "quota_exceeded"
              │
              └── SDK 收到心跳 → 更新本地缓存 → 下次调用前拦截
```

### 2.2 当前路径（v1.0 实现，心跳驱动）

```
Span 到达 management-server
      │
      ▼
TokenQuotaManager.RecordUsage()
      │
      ├── 写入 usage_logs
      ├── 更新 usage_summary
      ├── 检查 quota
      │     ├── warn_threshold 触发 → RiskAlert(severity:"info")
      │     ├── block_threshold 触发 → RiskAlert(severity:"medium")
      │     └── 更新 agent.risk_score
      │
      └── 下次 agent-runtime 心跳
            ├── HeartbeatResponse.suggested_action = "quota_exceeded"
            └── agent-runtime 收到 → 传递给 Supervisor → Hermes 拒绝后续调用
```

---

## 3. API 端（新增端点）

| Method | Path | 说明 |
|--------|------|------|
| GET | `/api/v1/quota/agents/{id}/usage` | 查询单个 Agent 的 Token 用量 |
| GET | `/api/v1/quota/family-groups/{id}/usage` | 查询 FamilyGroup 的 Token 用量 |
| GET | `/api/v1/quota/usage/summary` | 全局用量摘要（支持 ?period=daily&date=2026-05-19） |
| GET | `/api/v1/quota/prices` | 获取模型定价表 |
| POST | `/api/v1/quota/prices` | 新增 / 更新模型定价 |
| GET | `/api/v1/quota/quotas` | 列出配额规则（?target_type=agent&target_id=xxx） |
| POST | `/api/v1/quota/quotas` | 创建配额规则 |
| PUT | `/api/v1/quota/quotas/{id}` | 修改配额规则 |
| DELETE | `/api/v1/quota/quotas/{id}` | 删除配额规则 |
| GET | `/api/v1/quota/logs` | 查询 Token 消耗日志（支持 ?agent_id=&model=&from=&to=） |

---

## 4. 前端设计

### 4.1 新增页面：Token 用量仪表盘

```
management-server/web/src/pages/TokenUsagePage.tsx
```

**页面布局：**

```
┌──────────────────────────────────────────────────────┐
│  Token 算力配额管理                                    │
│                                                       │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐    │
│  │ 今日消耗      │ │ 本月消耗      │ │ 活跃 Agent   │    │
│  │ 1,234,567    │ │ 12,345,678  │ │ 12 / 15     │    │
│  │ tokens       │ │ tokens       │ │ 在线         │    │
│  │ ↑12% vs 昨天  │ │ $34.56 费用  │ │              │    │
│  └─────────────┘ └─────────────┘ └─────────────┘    │
│                                                       │
│  ┌──────────────────────────────────────────────────┐│
│  │ Token 消耗趋势 (AreaChart)                         ││
│  │ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  ││
│  │ 按日/周/月切换  ·  按 Agent 筛选  ·  按模型筛选     ││
│  └──────────────────────────────────────────────────┘│
│                                                       │
│  ┌──────────────────────┐ ┌──────────────────────────┐│
│  │ 模型消耗分布 (PieChart) │ │ Agent 消耗排行 (BarChart)  ││
│  │ GPT-4o: 45%           │ │ agent-001 ████████ 8.2M ││
│  │ Claude: 30%           │ │ agent-002 ██████ 5.1M   ││
│  │ DeepSeek: 25%         │ │ agent-003 ████ 3.4M     ││
│  └──────────────────────┘ └──────────────────────────┘│
└──────────────────────────────────────────────────────┘
```

### 4.2 新增页面：配额管理

```
management-server/web/src/pages/QuotaManagementPage.tsx
```

- 配额规则列表（表格：Target, 类型, 日限额, 月限额, 状态, 操作）
- 新建/编辑配额规则对话框
- 快捷操作：一键暂停所有 Agent / 一键恢复

### 4.3 扩展已有页面

- **AgentDetailPage** → 新增 "Token 用量" Tab，展示该 Agent 的用量曲线 + 配额状态
- **DashboardPage** → 统计卡片区新增 "今日 Token 消耗" + "成本估算"
- **AlertsPage** → 新增告警类型 "token_quota"，自动从 quota 超标事件生成

---

## 5. 数据流集成

### 5.1 Span 到达时的 Token 提取

现有的 span 数据结构中，token 信息在 `attributes` 字段中。在 `router.go` 的 `ingestSpans` 处理函数中扩展：

```go
func extractTokenUsage(attrs map[string]string) (model string, input int64, output int64, total int64) {
    model = attrs["gen_ai.response.model"]
    if model == "" {
        model = attrs["gen_ai.request.model"]
    }
    input, _ = strconv.ParseInt(attrs["gen_ai.usage.input_tokens"], 10, 64)
    output, _ = strconv.ParseInt(attrs["gen_ai.usage.output_tokens"], 10, 64)
    total, _ = strconv.ParseInt(attrs["gen_ai.usage.total_tokens"], 10, 64)
    if total == 0 {
        total = input + output
    }
    return
}
```

### 5.2 心跳响应扩展

在 `HeartbeatResponse` 中新增字段：

```go
type HeartbeatResponse struct {
    Acknowledged        bool   `json:"acknowledged"`
    LatestPolicyVersion string `json:"latest_policy_version"`
    SuggestedAction     string `json:"suggested_action"`
    // 新增
    QuotaStatus         string `json:"quota_status,omitempty"` // ok | warned | exceeded
    TokenUsageToday     int64  `json:"token_usage_today,omitempty"`
    TokenQuotaDaily     int64  `json:"token_quota_daily,omitempty"`
}
```

---

## 6. 部署配置

### 6.1 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AGENTSHIELD_TOKEN_QUOTA_ENABLED` | `true` | 启用 Token 配额管理 |
| `AGENTSHIELD_TOKEN_QUOTA_DEFAULT_DAILY` | `1000000` | 新 Agent 默认日配额（1M tokens） |
| `AGENTSHIELD_TOKEN_QUOTA_DEFAULT_MONTHLY` | `20000000` | 新 Agent 默认月配额（20M tokens） |
| `AGENTSHIELD_TOKEN_QUOTA_AGGREGATION_INTERVAL` | `300` | 用量聚合间隔（秒） |
| `AGENTSHIELD_TOKEN_QUOTA_LOG_RETENTION_DAYS` | `90` | 日志保留天数 |

### 6.2 Docker Compose 变更

Token 配额管理模块运行在 management-server 进程内，不需要独立服务。SQLite 表通过 migration 自动创建。

---

## 7. 测试方案

### 单元测试（Go）

| 测试用例 | 覆盖 |
|---------|------|
| `TestExtractTokenUsage` | 从 span attributes 正确提取 token 字段 |
| `TestCostCalculation` | 多模型定价计算正确性（毫分精度） |
| `TestQuotaCheckWarn` | 用量达到 80% 时触发 warn |
| `TestQuotaCheckBlock` | 用量达到 100% 时触发 block |
| `TestQuotaCheckDaily` | 跨天后日配额自动重置 |
| `TestFamilyGroupQuotaInheritance` | Agent 无独立配额时使用 FamilyGroup 配额 |
| `TestPriorityPreemption` | 高负载时低优先级 Agent 先被限流 |
| `TestUsageAggregation` | 聚合任务正确汇总 daily/weekly/monthly |
| `TestConcurrentQuotaUpdate` | 并发写入时配额计数不丢失/不重复 |

### 集成测试（Python）

```python
# tests/integration/test_token_quota.py

def test_quota_warn_on_threshold():
    """Agent token 消耗达到 80% 日配额时生成 info 告警"""

def test_quota_block_on_exceed():
    """Agent token 消耗超过日配额时心跳返回 quota_exceeded"""

def test_quota_daily_reset():
    """跨日后 Token 计数重新开始，昨日数据保留在 summary"""

def test_multi_model_usage_tracking():
    """同一 Agent 使用不同模型，用量按模型分别统计"""

def test_cost_estimation_accuracy():
    """验证 token 用量 → 成本换算与真实账单偏差 < 1%"""
```

### 性能基准

| 指标 | 目标值 |
|------|--------|
| RecordUsage() 单次延迟 | < 5ms |
| 1000 Agent 并发写入 QPS | > 5000 |
| 聚合任务执行时间（1M 条 log） | < 30s |
| 前端用量曲线加载（30d 数据） | < 500ms |

---

## 8. 实施路线

| 阶段 | 内容 | 预计工时 |
|------|------|---------|
| **Phase 1** | 数据模型：SQL migration + Go models + `model_prices` 预填 | 1d |
| **Phase 2** | `TokenQuotaManager` 核心：RecordUsage / CheckQuota / CostCalc | 2d |
| **Phase 3** | API 层：quota CRUD + usage 查询端点 | 1.5d |
| **Phase 4** | 心跳集成：quota_status 字段 + 超标响应 | 0.5d |
| **Phase 5** | 前端：TokenUsagePage + QuotaManagementPage + 扩展现有页面 | 2d |
| **Phase 6** | 测试：单元测试 + 集成测试 + 性能基准 | 1.5d |
| **Phase 7** | 文档 + Docker Compose 集成 | 0.5d |
| **合计** | | **~9d** |
