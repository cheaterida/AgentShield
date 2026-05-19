package store

import (
	"context"
	"testing"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
)

func newTestSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite(:memory:) failed: %v", err)
	}
	return s
}

// ── Agent CRUD ──

func TestSQLite_UpsertAndGetAgent(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	agent := models.Agent{
		ID: "a1", FamilyGroupID: "fg1", DisplayName: "Test Agent",
		Status: "online", RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	got, err := s.GetAgent(ctx, "a1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.ID != "a1" {
		t.Errorf("got ID %s, want a1", got.ID)
	}
	if got.DisplayName != "Test Agent" {
		t.Errorf("got DisplayName %s", got.DisplayName)
	}
}

func TestSQLite_ListAgents(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		id := "a" + string(rune('0'+i))
		a := models.Agent{
			ID: id, FamilyGroupID: "fg", Status: "online",
			RegisteredAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := s.UpsertAgent(ctx, a); err != nil {
			t.Fatalf("UpsertAgent(%s): %v", id, err)
		}
	}

	agents, err := s.ListAgents(ctx, "fg")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
	}
}

func TestSQLite_UpdateAgentStatus(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	a := models.Agent{
		ID: "a1", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	}
	s.UpsertAgent(ctx, a)

	if err := s.UpdateAgentStatus(ctx, "a1", "degraded"); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	got, _ := s.GetAgent(ctx, "a1")
	if got.Status != "degraded" {
		t.Errorf("expected degraded, got %s", got.Status)
	}
}

func TestSQLite_UpdateAgentHeartbeat(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	a := models.Agent{
		ID: "a1", FamilyGroupID: "fg", Status: "offline",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	}
	s.UpsertAgent(ctx, a)

	if err := s.UpdateAgentHeartbeat(ctx, "a1"); err != nil {
		t.Fatalf("UpdateAgentHeartbeat: %v", err)
	}

	got, _ := s.GetAgent(ctx, "a1")
	if got.Status != "online" {
		t.Errorf("expected online after heartbeat, got %s", got.Status)
	}
	if got.LastHeartbeatAt == nil {
		t.Error("expected non-nil LastHeartbeatAt")
	}
}

// ── FamilyGroup CRUD ──

func TestSQLite_FamilyGroupCRUD(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	fg := models.FamilyGroup{
		ID: "fg1", DisplayName: "Test FG",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.CreateFamilyGroup(ctx, fg); err != nil {
		t.Fatalf("CreateFamilyGroup: %v", err)
	}

	got, err := s.GetFamilyGroup(ctx, "fg1")
	if err != nil {
		t.Fatalf("GetFamilyGroup: %v", err)
	}
	if got.DisplayName != "Test FG" {
		t.Errorf("got %s, want Test FG", got.DisplayName)
	}

	groups, err := s.ListFamilyGroups(ctx)
	if err != nil {
		t.Fatalf("ListFamilyGroups: %v", err)
	}
	if len(groups) < 1 {
		t.Error("expected at least 1 family group")
	}

	fg.DisplayName = "Updated FG"
	fg.UpdatedAt = time.Now()
	if err := s.UpdateFamilyGroup(ctx, fg); err != nil {
		t.Fatalf("UpdateFamilyGroup: %v", err)
	}

	if err := s.DeleteFamilyGroup(ctx, "fg1"); err != nil {
		t.Fatalf("DeleteFamilyGroup: %v", err)
	}

	_, err = s.GetFamilyGroup(ctx, "fg1")
	if err == nil {
		t.Error("expected error after delete")
	}
}

// ── Audit Events ──

func TestSQLite_AuditEvents(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	events := []models.AuditEvent{
		{EventID: "evt1", AgentID: "a1", Action: "read", OccurredAt: time.Now()},
		{EventID: "evt2", AgentID: "a1", Action: "write", OccurredAt: time.Now()},
	}
	n, err := s.AppendAuditEvents(ctx, events)
	if err != nil {
		t.Fatalf("AppendAuditEvents: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 appended, got %d", n)
	}

	all, err := s.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 events, got %d", len(all))
	}

	filtered, err := s.ListAuditEventsFiltered(ctx, models.AuditEventFilter{
		AgentID: "a1", Action: "write", Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAuditEventsFiltered: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 filtered event, got %d", len(filtered))
	}
}

// ── Risk Alerts ──

func TestSQLite_RiskAlerts(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	alert := models.RiskAlert{
		AlertID: "alert1", FamilyGroupID: "fg1", AgentID: "a1",
		Severity: "high", Title: "Test Alert",
		Status: "open", OccurredAt: time.Now(),
	}
	if err := s.CreateRiskAlert(ctx, alert); err != nil {
		t.Fatalf("CreateRiskAlert: %v", err)
	}

	alerts, err := s.ListRiskAlerts(ctx, models.AlertFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListRiskAlerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(alerts))
	}

	if err := s.UpdateRiskAlertStatus(ctx, "alert1", "acknowledged"); err != nil {
		t.Fatalf("UpdateRiskAlertStatus: %v", err)
	}

	filtered, _ := s.ListRiskAlerts(ctx, models.AlertFilter{Status: "open", Limit: 10})
	if len(filtered) != 0 {
		t.Errorf("expected 0 open alerts after ack, got %d", len(filtered))
	}
}

// ── Policy Bundles ──

func TestSQLite_PolicyBundles(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	bundle := models.PolicyBundle{
		Version: "v1.0.0", PolicyType: "opa_rego",
		Payload: []byte("package test"), Digest: "sha256:abc",
		Active: true, CreatedAt: time.Now(),
	}
	if err := s.CreatePolicyBundle(ctx, bundle); err != nil {
		t.Fatalf("CreatePolicyBundle: %v", err)
	}

	active, err := s.GetActivePolicyBundle(ctx)
	if err != nil {
		t.Fatalf("GetActivePolicyBundle: %v", err)
	}
	if active.Version != "v1.0.0" {
		t.Errorf("expected v1.0.0, got %s", active.Version)
	}

	bundles, err := s.ListPolicyBundles(ctx)
	if err != nil {
		t.Fatalf("ListPolicyBundles: %v", err)
	}
	if len(bundles) != 1 {
		t.Errorf("expected 1 bundle, got %d", len(bundles))
	}
}

// ── Dashboard Stats ──

func TestSQLite_DashboardStats(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	a := models.Agent{
		ID: "a1", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	}
	s.UpsertAgent(ctx, a)

	stats, err := s.GetDashboardStats(ctx, "fg")
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats.AgentCount != 1 {
		t.Errorf("expected 1 agent, got %d", stats.AgentCount)
	}
	if stats.OnlineAgentCount != 1 {
		t.Errorf("expected 1 online, got %d", stats.OnlineAgentCount)
	}
}
