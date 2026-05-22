package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
)

func TestMemory_UpsertAndGetAgent(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	agent := models.Agent{
		ID: "a1", FamilyGroupID: "fg1", DisplayName: "Mem Agent",
		Status: "online", RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	got, found, err := s.GetAgent(ctx, "a1")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if !found {
		t.Fatal("GetAgent: agent not found")
	}
	if got.ID != "a1" {
		t.Errorf("got ID %s, want a1", got.ID)
	}
}

func TestMemory_ListAgents_FilterByFamilyGroup(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	s.UpsertAgent(ctx, models.Agent{
		ID: "a1", FamilyGroupID: "fg1", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})
	s.UpsertAgent(ctx, models.Agent{
		ID: "a2", FamilyGroupID: "fg2", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	fg1Agents, _ := s.ListAgents(ctx, "fg1")
	if len(fg1Agents) != 1 {
		t.Errorf("fg1: expected 1, got %d", len(fg1Agents))
	}

	allAgents, _ := s.ListAgents(ctx, "")
	if len(allAgents) != 2 {
		t.Errorf("all: expected 2, got %d", len(allAgents))
	}
}

func TestMemory_ConcurrentSafety(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := "agent-" + string(rune('0'+idx%10))
			s.UpsertAgent(ctx, models.Agent{
				ID: id, FamilyGroupID: "fg", Status: "online",
				RegisteredAt: time.Now(), UpdatedAt: time.Now(),
			})
			s.GetAgent(ctx, id)
			s.ListAgents(ctx, "fg")
		}(i)
	}
	wg.Wait()
}

func TestMemory_AuditEvents_RingBufferCap(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	for i := 0; i < 15000; i++ {
		s.AppendAuditEvents(ctx, []models.AuditEvent{
			{EventID: "evt-" + string(rune(i)), OccurredAt: time.Now()},
		})
	}

	events, err := s.ListAuditEvents(ctx, 100)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) > 100 {
		t.Errorf("expected at most 100, got %d", len(events))
	}
}

func TestMemory_FamilyGroupCRUD(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	fg := models.FamilyGroup{
		ID: "fg1", DisplayName: "FG1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.CreateFamilyGroup(ctx, fg); err != nil {
		t.Fatalf("CreateFamilyGroup: %v", err)
	}

	got, _, err := s.GetFamilyGroup(ctx, "fg1")
	if err != nil {
		t.Fatalf("GetFamilyGroup: %v", err)
	}
	if got.DisplayName != "FG1" {
		t.Errorf("wrong name: %s", got.DisplayName)
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

func TestMemory_RiskAlerts(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	alert := models.RiskAlert{
		AlertID: "alert1", FamilyGroupID: "fg1",
		Severity: "critical", Title: "Critical Alert",
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

	if err := s.UpdateRiskAlertStatus(ctx, "alert1", "resolved"); err != nil {
		t.Fatalf("UpdateRiskAlertStatus: %v", err)
	}

	filtered, _, err := s.ListRiskAlerts(ctx, models.AlertFilter{Status: "resolved", Limit: 10})
	if err != nil {
		t.Fatalf("ListRiskAlerts: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 resolved, got %d", len(filtered))
	}
}

func TestMemory_DashboardStats(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	s.UpsertAgent(ctx, models.Agent{
		ID: "a1", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})
	s.UpsertAgent(ctx, models.Agent{
		ID: "a2", FamilyGroupID: "fg", Status: "suspicious",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	stats, err := s.GetDashboardStats(ctx, "fg")
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}
	if stats.AgentCount != 2 {
		t.Errorf("expected 2 agents, got %d", stats.AgentCount)
	}
	if stats.OnlineAgentCount != 1 {
		t.Errorf("expected 1 online, got %d", stats.OnlineAgentCount)
	}
	if stats.SuspiciousAgentCount != 1 {
		t.Errorf("expected 1 suspicious, got %d", stats.SuspiciousAgentCount)
	}
}

// ── Token Quota ──

func TestMemory_TokenQuotaCRUD(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	q := models.TokenQuota{
		QuotaID: "q1", TargetType: "agent", TargetID: "agent-001",
		DailyLimit: 1000000, MonthlyLimit: 20000000,
		WarnThreshold: 0.8, BlockThreshold: 1.0, Priority: 5, Active: true,
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

	q.DailyLimit = 500000
	if err := s.UpdateTokenQuota(ctx, q); err != nil {
		t.Fatalf("UpdateTokenQuota: %v", err)
	}
	got, _, _ = s.GetTokenQuota(ctx, "agent", "agent-001")
	if got.DailyLimit != 500000 {
		t.Errorf("got DailyLimit %d after update, want 500000", got.DailyLimit)
	}

	quotas, err := s.ListTokenQuotas(ctx, "agent")
	if err != nil {
		t.Fatalf("ListTokenQuotas: %v", err)
	}
	if len(quotas) != 1 {
		t.Errorf("expected 1 quota, got %d", len(quotas))
	}

	if err := s.DeleteTokenQuota(ctx, "q1"); err != nil {
		t.Fatalf("DeleteTokenQuota: %v", err)
	}
	_, found, _ = s.GetTokenQuota(ctx, "agent", "agent-001")
	if found {
		t.Error("expected quota not found after delete")
	}
}

func TestMemory_TokenUsageLogs(t *testing.T) {
	s := NewMemory(0)
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

func TestMemory_TokenUsageSummary(t *testing.T) {
	s := NewMemory(0)
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

func TestMemory_ModelPrices(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()

	prices, err := s.ListModelPrices(ctx)
	if err != nil {
		t.Fatalf("ListModelPrices: %v", err)
	}
	if len(prices) != 0 {
		t.Errorf("expected 0 prices initially, got %d", len(prices))
	}

	p := models.ModelPrice{
		ModelID: "gpt-4o", Provider: "openai", DisplayName: "GPT-4o",
		InputPriceMillicents: 250000, OutputPriceMillicents: 1000000, Active: true,
	}
	if err := s.UpsertModelPrice(ctx, p); err != nil {
		t.Fatalf("UpsertModelPrice: %v", err)
	}

	prices, err = s.ListModelPrices(ctx)
	if err != nil {
		t.Fatalf("ListModelPrices: %v", err)
	}
	if len(prices) != 1 {
		t.Fatalf("expected 1 price, got %d", len(prices))
	}
	if prices[0].ModelID != "gpt-4o" {
		t.Errorf("got model %s", prices[0].ModelID)
	}
}

func TestMemory_TokenQuotaConcurrent(t *testing.T) {
	s := NewMemory(0)
	ctx := context.Background()
	var wg sync.WaitGroup

	s.CreateTokenQuota(ctx, models.TokenQuota{
		QuotaID: "q1", TargetType: "agent", TargetID: "agent-001",
		DailyLimit: 1000000, Active: true,
	})

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.GetTokenQuota(ctx, "agent", "agent-001")
			s.ListTokenQuotas(ctx, "agent")
			s.AppendTokenUsageLog(ctx, models.TokenUsageLog{
				LogID:         fmt.Sprintf("clog-%d", idx),
				AgentID:       "agent-001",
				TotalTokens:   int64(idx * 100),
				CostMillicents: int64(idx * 50),
				OccurredAt:    time.Now().UTC().Format(time.RFC3339Nano),
			})
		}(i)
	}
	wg.Wait()

	logs, total, err := s.GetTokenUsageLogs(ctx, models.TokenUsageLogFilter{AgentID: "agent-001", Limit: 100})
	if err != nil {
		t.Fatalf("GetTokenUsageLogs: %v", err)
	}
	if total != 20 {
		t.Errorf("expected 20 logs, got %d", total)
	}
	_ = logs
}
