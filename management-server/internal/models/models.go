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
	SuggestedAction     string `json:"suggested_action"` // ok|update_policy|restart_probe|isolate
}
