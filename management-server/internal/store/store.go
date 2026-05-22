package store

import (
	"context"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
)

type Store interface {
	UpsertAgent(ctx context.Context, a models.Agent) error
	ListAgents(ctx context.Context, familyGroupID string) ([]models.Agent, error)
	GetAgent(ctx context.Context, id string) (models.Agent, bool, error)
	AppendAuditEvents(ctx context.Context, events []models.AuditEvent) (int, error)
	ListAuditEvents(ctx context.Context, limit int) ([]models.AuditEvent, error)

	// FamilyGroup CRUD
	CreateFamilyGroup(ctx context.Context, fg models.FamilyGroup) error
	GetFamilyGroup(ctx context.Context, id string) (models.FamilyGroup, bool, error)
	ListFamilyGroups(ctx context.Context) ([]models.FamilyGroup, error)
	UpdateFamilyGroup(ctx context.Context, fg models.FamilyGroup) error
	DeleteFamilyGroup(ctx context.Context, id string) error

	// Agent status management
	UpdateAgentStatus(ctx context.Context, id string, status string) error
	UpdateAgentHeartbeat(ctx context.Context, id string) error
	MarkStaleAgentsOffline(ctx context.Context, timeout time.Duration) (int, error)
	ListAgentsByStatus(ctx context.Context, status string) ([]models.Agent, error)
	UpdateAgentRiskScore(ctx context.Context, id string, score float64) error

	// Audit events with filters
	ListAuditEventsFiltered(ctx context.Context, filter models.AuditEventFilter) ([]models.AuditEvent, int, error)

	// Policy bundles
	CreatePolicyBundle(ctx context.Context, pb models.PolicyBundle) error
	GetActivePolicyBundle(ctx context.Context) (models.PolicyBundle, bool, error)
	ListPolicyBundles(ctx context.Context) ([]models.PolicyBundle, error)
	SetPolicyBundleActive(ctx context.Context, version string) error

	// Risk alerts
	CreateRiskAlert(ctx context.Context, alert models.RiskAlert) error
	ListRiskAlerts(ctx context.Context, filter models.AlertFilter) ([]models.RiskAlert, int, error)
	UpdateRiskAlertStatus(ctx context.Context, alertID string, status string) error

	// Dashboard
	GetDashboardStats(ctx context.Context, familyGroupID string) (models.DashboardStats, error)

	// ── Token Quota ──

	CreateTokenQuota(ctx context.Context, q models.TokenQuota) error
	GetTokenQuota(ctx context.Context, targetType, targetID string) (models.TokenQuota, bool, error)
	ListTokenQuotas(ctx context.Context, targetType string) ([]models.TokenQuota, error)
	UpdateTokenQuota(ctx context.Context, q models.TokenQuota) error
	DeleteTokenQuota(ctx context.Context, quotaID string) error

	// ── Token Usage Logs ──

	AppendTokenUsageLog(ctx context.Context, log models.TokenUsageLog) error
	GetTokenUsageLogs(ctx context.Context, filter models.TokenUsageLogFilter) ([]models.TokenUsageLog, int, error)

	// ── Token Usage Summary ──

	GetTokenUsageSummary(ctx context.Context, targetType, targetID, period string) ([]models.TokenUsageSummary, error)
	UpsertTokenUsageSummary(ctx context.Context, s models.TokenUsageSummary) error

	// ── Model Prices ──

	ListModelPrices(ctx context.Context) ([]models.ModelPrice, error)
	UpsertModelPrice(ctx context.Context, p models.ModelPrice) error

	// ── Approval Requests (Track B3) ──

	CreateApprovalRequest(ctx context.Context, req models.ApprovalRequest) error
	GetApprovalRequest(ctx context.Context, requestID string) (models.ApprovalRequest, bool, error)
	ListApprovalRequests(ctx context.Context, filter models.ApprovalFilter) ([]models.ApprovalRequest, int, error)
	UpdateApprovalRequest(ctx context.Context, req models.ApprovalRequest) error
}
