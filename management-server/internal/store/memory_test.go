package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
)

func TestMemory_UpsertAndGetAgent(t *testing.T) {
	s := NewMemory()
	ctx := context.Background()

	agent := models.Agent{
		ID: "a1", FamilyGroupID: "fg1", DisplayName: "Mem Agent",
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
}

func TestMemory_ListAgents_FilterByFamilyGroup(t *testing.T) {
	s := NewMemory()
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
	s := NewMemory()
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
	s := NewMemory()
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
	s := NewMemory()
	ctx := context.Background()

	fg := models.FamilyGroup{
		ID: "fg1", DisplayName: "FG1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.CreateFamilyGroup(ctx, fg); err != nil {
		t.Fatalf("CreateFamilyGroup: %v", err)
	}

	got, _ := s.GetFamilyGroup(ctx, "fg1")
	if got.DisplayName != "FG1" {
		t.Errorf("wrong name: %s", got.DisplayName)
	}

	if err := s.DeleteFamilyGroup(ctx, "fg1"); err != nil {
		t.Fatalf("DeleteFamilyGroup: %v", err)
	}

	_, err := s.GetFamilyGroup(ctx, "fg1")
	if err == nil {
		t.Error("expected error for deleted group")
	}
}

func TestMemory_RiskAlerts(t *testing.T) {
	s := NewMemory()
	ctx := context.Background()

	alert := models.RiskAlert{
		AlertID: "alert1", FamilyGroupID: "fg1",
		Severity: "critical", Title: "Critical Alert",
		Status: "open", OccurredAt: time.Now(),
	}
	if err := s.CreateRiskAlert(ctx, alert); err != nil {
		t.Fatalf("CreateRiskAlert: %v", err)
	}

	alerts, _ := s.ListRiskAlerts(ctx, models.AlertFilter{Limit: 10})
	if len(alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(alerts))
	}

	if err := s.UpdateRiskAlertStatus(ctx, "alert1", "resolved"); err != nil {
		t.Fatalf("UpdateRiskAlertStatus: %v", err)
	}

	filtered, _ := s.ListRiskAlerts(ctx, models.AlertFilter{Status: "resolved", Limit: 10})
	if len(filtered) != 1 {
		t.Errorf("expected 1 resolved, got %d", len(filtered))
	}
}

func TestMemory_DashboardStats(t *testing.T) {
	s := NewMemory()
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
