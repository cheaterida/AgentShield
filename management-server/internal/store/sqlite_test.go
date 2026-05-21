package store

import (
	"context"
	"fmt"
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

	got, _, err := s.GetAgent(ctx, "a1")
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

	got, _, _ := s.GetAgent(ctx, "a1")
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

	got, _, _ := s.GetAgent(ctx, "a1")
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

	got, _, err := s.GetFamilyGroup(ctx, "fg1")
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

	_, found, err := s.GetFamilyGroup(ctx, "fg1")
	if err != nil {
		t.Fatalf("GetFamilyGroup after delete: %v", err)
	}
	if found {
		t.Error("expected family group not found after delete")
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

	filtered, _, err := s.ListAuditEventsFiltered(ctx, models.AuditEventFilter{
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

	alerts, _, err := s.ListRiskAlerts(ctx, models.AlertFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListRiskAlerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(alerts))
	}

	if err := s.UpdateRiskAlertStatus(ctx, "alert1", "acknowledged"); err != nil {
		t.Fatalf("UpdateRiskAlertStatus: %v", err)
	}

	filtered, _, err := s.ListRiskAlerts(ctx, models.AlertFilter{Status: "open", Limit: 10})
	if err != nil {
		t.Fatalf("ListRiskAlerts: %v", err)
	}
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

	active, _, err := s.GetActivePolicyBundle(ctx)
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

// ── Token Quota CRUD ──

func TestSQLite_TokenQuotaCRUD(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	q := models.TokenQuota{
		QuotaID:    "q1",
		TargetType: "agent",
		TargetID:   "agent-001",
		QuotaName:  "default",
		DailyLimit: 1000000, MonthlyLimit: 20000000,
		WarnThreshold: 0.8, BlockThreshold: 1.0,
		Priority: 5, Active: true,
	}
	if err := s.CreateTokenQuota(ctx, q); err != nil {
		t.Fatalf("CreateTokenQuota: %v", err)
	}

	got, found, err := s.GetTokenQuota(ctx, "agent", "agent-001")
	if err != nil {
		t.Fatalf("GetTokenQuota: %v", err)
	}
	if !found {
		t.Fatal("expected quota to be found")
	}
	if got.DailyLimit != 1000000 {
		t.Errorf("got DailyLimit %d, want 1000000", got.DailyLimit)
	}

	// Update
	q.DailyLimit = 500000
	if err := s.UpdateTokenQuota(ctx, q); err != nil {
		t.Fatalf("UpdateTokenQuota: %v", err)
	}
	got, _, _ = s.GetTokenQuota(ctx, "agent", "agent-001")
	if got.DailyLimit != 500000 {
		t.Errorf("got DailyLimit %d after update, want 500000", got.DailyLimit)
	}

	// List
	quotas, err := s.ListTokenQuotas(ctx, "agent")
	if err != nil {
		t.Fatalf("ListTokenQuotas: %v", err)
	}
	if len(quotas) != 1 {
		t.Errorf("expected 1 quota, got %d", len(quotas))
	}

	// Delete
	if err := s.DeleteTokenQuota(ctx, "q1"); err != nil {
		t.Fatalf("DeleteTokenQuota: %v", err)
	}
	_, found, _ = s.GetTokenQuota(ctx, "agent", "agent-001")
	if found {
		t.Error("expected quota not found after delete")
	}
}

// ── Token Usage Logs ──

func TestSQLite_TokenUsageLogs(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	log := models.TokenUsageLog{
		LogID: "log1", AgentID: "a1", FamilyGroupID: "fg1",
		SpanID: "span1", ModelName: "gpt-4o",
		InputTokens: 100, OutputTokens: 50, TotalTokens: 150,
		CostMillicents: 75000, QuotaStatus: "ok",
		OccurredAt: "2026-05-20T10:00:00Z",
	}
	if err := s.AppendTokenUsageLog(ctx, log); err != nil {
		t.Fatalf("AppendTokenUsageLog: %v", err)
	}

	logs, total, err := s.GetTokenUsageLogs(ctx, models.TokenUsageLogFilter{AgentID: "a1", Limit: 10})
	if err != nil {
		t.Fatalf("GetTokenUsageLogs: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ModelName != "gpt-4o" {
		t.Errorf("got model %s", logs[0].ModelName)
	}
}

func TestSQLite_TokenUsageLogFilter(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		l := models.TokenUsageLog{
			LogID:         fmt.Sprintf("log%d", i+1),
			AgentID:       "a1",
			FamilyGroupID: "fg1",
			SpanID:        fmt.Sprintf("span%d", i+1),
			ModelName:     "gpt-4o",
			InputTokens:   int64((i + 1) * 100),
			TotalTokens:   int64((i + 1) * 150),
			CostMillicents: int64((i + 1) * 75000),
			QuotaStatus:   "ok",
			OccurredAt:    fmt.Sprintf("2026-05-%02dT10:00:00Z", 20+i),
		}
		s.AppendTokenUsageLog(ctx, l)
	}

	// Test time range filter
	logs, total, err := s.GetTokenUsageLogs(ctx, models.TokenUsageLogFilter{
		FromTime: "2026-05-21T00:00:00Z",
		ToTime:   "2026-05-23T00:00:00Z",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("GetTokenUsageLogs: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 logs in range, got %d", total)
	}
	if len(logs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(logs))
	}
}

// ── Token Usage Summary ──

func TestSQLite_TokenUsageSummary(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	sum := models.TokenUsageSummary{
		TargetType: "agent", TargetID: "a1",
		Period: "daily", DateKey: "2026-05-20",
		InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500,
		RequestCount: 10, CostMillicents: 750000,
	}
	if err := s.UpsertTokenUsageSummary(ctx, sum); err != nil {
		t.Fatalf("UpsertTokenUsageSummary: %v", err)
	}

	// Second upsert should add to existing
	if err := s.UpsertTokenUsageSummary(ctx, sum); err != nil {
		t.Fatalf("UpsertTokenUsageSummary (2nd): %v", err)
	}

	summaries, err := s.GetTokenUsageSummary(ctx, "agent", "a1", "daily")
	if err != nil {
		t.Fatalf("GetTokenUsageSummary: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].TotalTokens != 3000 {
		t.Errorf("expected TotalTokens=3000 (after upsert merge), got %d", summaries[0].TotalTokens)
	}
	if summaries[0].RequestCount != 20 {
		t.Errorf("expected RequestCount=20 (after upsert merge), got %d", summaries[0].RequestCount)
	}
}

// ── Model Prices ──

func TestSQLite_ModelPrices(t *testing.T) {
	s := newTestSQLite(t)
	ctx := context.Background()

	prices, err := s.ListModelPrices(ctx)
	if err != nil {
		t.Fatalf("ListModelPrices: %v", err)
	}
	// Seed data from migration
	if len(prices) == 0 {
		t.Fatal("expected model prices from seed data")
	}

	// Upsert new
	p := models.ModelPrice{
		ModelID:              "custom-model",
		Provider:             "custom",
		DisplayName:          "Custom Model",
		InputPriceMillicents: 100000,
		OutputPriceMillicents: 400000,
		Active:               true,
	}
	if err := s.UpsertModelPrice(ctx, p); err != nil {
		t.Fatalf("UpsertModelPrice: %v", err)
	}

	prices, err = s.ListModelPrices(ctx)
	if err != nil {
		t.Fatalf("ListModelPrices: %v", err)
	}
	found := false
	for _, p := range prices {
		if p.ModelID == "custom-model" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected custom-model in price list")
	}
}
