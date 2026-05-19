package store

import (
	"context"

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
}
