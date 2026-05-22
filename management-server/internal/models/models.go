package models

import "time"

// ── Agent ──

type Agent struct {
	ID              string            `json:"id"`
	FamilyGroupID   string            `json:"family_group_id"`
	DisplayName     string            `json:"display_name,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Status          string            `json:"status"` // online|offline|suspicious|degraded|unknown
	RiskScore       float64           `json:"risk_score"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at,omitempty"`
	RegisteredAt    time.Time         `json:"registered_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type RegisterAgentRequest struct {
	ID            string            `json:"id"`
	FamilyGroupID string            `json:"family_group_id"`
	DisplayName   string            `json:"display_name,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type UpdateAgentStatusRequest struct {
	Status string `json:"status"`
}

// ── AuditEvent ──

type AuditEvent struct {
	EventID          string            `json:"event_id"`
	OccurredAt       time.Time         `json:"occurred_at"`
	FamilyGroupID    string            `json:"family_group_id"`
	AgentID          string            `json:"agent_id"`
	ResourceRef      string            `json:"resource_ref,omitempty"`
	Action           string            `json:"action,omitempty"`
	Attributes       map[string]string `json:"attributes,omitempty"`
	RiskContribution float64           `json:"risk_contribution,omitempty"`
}

type AuditEventFilter struct {
	AgentID       string
	FamilyGroupID string
	Action        string
	FromTime      *time.Time
	ToTime        *time.Time
	Limit         int
	Offset        int
}

// ── FamilyGroup ──

type FamilyGroup struct {
	ID                  string            `json:"id"`
	DisplayName         string            `json:"display_name,omitempty"`
	MemberPrincipalIDs  []string          `json:"member_principal_ids,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// ── PolicyBundle ──

type PolicyBundle struct {
	Version    string            `json:"version"`
	PolicyType string            `json:"policy_type"` // opa_rego|gnn_policy|gnn_policy_light
	Payload    []byte            `json:"payload"`
	Digest     string            `json:"digest"`
	Active     bool              `json:"active"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// ── RiskAlert ──

type RiskAlert struct {
	AlertID       string            `json:"alert_id"`
	FamilyGroupID string            `json:"family_group_id"`
	AgentID       string            `json:"agent_id,omitempty"`
	Severity      string            `json:"severity"` // low|medium|high|critical
	Title         string            `json:"title"`
	Description   string            `json:"description,omitempty"`
	Status        string            `json:"status"` // open|acknowledged|resolved|dismissed
	Metadata      map[string]string `json:"metadata,omitempty"`
	OccurredAt    time.Time         `json:"occurred_at"`
	ResolvedAt    *time.Time        `json:"resolved_at,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

type AlertFilter struct {
	FamilyGroupID string
	Severity      string
	Status        string
	Limit         int
	Offset        int
}

// ── Dashboard ──

type DashboardStats struct {
	AgentCount          int         `json:"agent_count"`
	OnlineAgentCount    int         `json:"online_agent_count"`
	SuspiciousAgentCount int        `json:"suspicious_agent_count"`
	EventRateLastHour   int         `json:"event_rate_last_hour"`
	OpenAlertCount      int         `json:"open_alert_count"`
	CriticalAlertCount  int         `json:"critical_alert_count"`
	RecentAlerts        []RiskAlert `json:"recent_alerts"`
}

// ── Heartbeat ──

type AgentHeartbeat struct {
	AgentID             string  `json:"agent_id"`
	CPUPercent          float64 `json:"cpu_percent"`
	MemoryBytes         uint64  `json:"memory_bytes"`
	ActiveProbes        int32   `json:"active_probes"`
	LocalPolicyVersion  string  `json:"local_policy_version"`
	BufferedEventCount  int32   `json:"buffered_event_count"`
}

type HeartbeatResponse struct {
	Acknowledged        bool   `json:"acknowledged"`
	LatestPolicyVersion string `json:"latest_policy_version"`
	SuggestedAction     string `json:"suggested_action"` // ok|update_policy|restart_probe|isolate|quota_exceeded
	QuotaStatus         string `json:"quota_status,omitempty"`
	TokenUsageToday     int64  `json:"token_usage_today,omitempty"`
	TokenQuotaDaily     int64  `json:"token_quota_daily,omitempty"`
}

// ── Token Quota ──

type TokenQuota struct {
	QuotaID          string  `json:"quota_id"`
	TargetType       string  `json:"target_type"`    // agent|family_group
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
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	// 运行时计算字段（不持久化）
	DailyUsed        int64   `json:"daily_used,omitempty"`
	MonthlyUsed      int64   `json:"monthly_used,omitempty"`
}

type TokenUsageLog struct {
	LogID            string `json:"log_id"`
	AgentID          string `json:"agent_id"`
	FamilyGroupID    string `json:"family_group_id"`
	SpanID           string `json:"span_id"`
	TraceID          string `json:"trace_id,omitempty"`
	ModelName        string `json:"model_name"`
	Provider         string `json:"provider,omitempty"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	CostMillicents   int64  `json:"cost_millicents"`
	QuotaStatus      string `json:"quota_status"`
	OccurredAt       string `json:"occurred_at"`
	CreatedAt        string `json:"created_at,omitempty"`
}

type TokenUsageSummary struct {
	TargetType    string `json:"target_type"`
	TargetID      string `json:"target_id"`
	Period        string `json:"period"`    // daily|weekly|monthly|total
	DateKey       string `json:"date_key"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	TotalTokens   int64  `json:"total_tokens"`
	RequestCount  int64  `json:"request_count"`
	CostMillicents int64 `json:"cost_millicents"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type ModelPrice struct {
	ModelID                  string `json:"model_id"`
	Provider                 string `json:"provider"`
	DisplayName              string `json:"display_name,omitempty"`
	InputPriceMillicents     int64  `json:"input_price_millicents"`
	OutputPriceMillicents    int64  `json:"output_price_millicents"`
	CacheReadPriceMillicents int64  `json:"cache_read_price_millicents,omitempty"`
	Active                   bool   `json:"active"`
	UpdatedAt                string `json:"updated_at,omitempty"`
}

// TokenUsageLogFilter 用于查询 token_usage_logs 的过滤参数
type TokenUsageLogFilter struct {
	AgentID       string
	FamilyGroupID string
	ModelName     string
	FromTime      string
	ToTime        string
	Limit         int
	Offset        int
}

// ── Approval (Track B3) ──

type ApprovalRequest struct {
	RequestID     string            `json:"request_id"`
	AgentID       string            `json:"agent_id"`
	FamilyGroupID string            `json:"family_group_id"`
	Action        string            `json:"action"`
	ResourceRef   string            `json:"resource_ref"`
	RiskScore     float64           `json:"risk_score"`
	Tier          string            `json:"tier"`   // department|security|ciso
	Status        string            `json:"status"` // pending|approved|denied|expired
	RequestedBy   string            `json:"requested_by"`
	ApprovedBy    string            `json:"approved_by,omitempty"`
	ApprovedAt    *time.Time        `json:"approved_at,omitempty"`
	ExpiresAt     *time.Time        `json:"expires_at,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

type ApprovalFilter struct {
	FamilyGroupID string
	Status        string
	Tier          string
	Limit         int
	Offset        int
}
