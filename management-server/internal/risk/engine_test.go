package risk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func makeTestEvent(agentID, action, resource string) models.AuditEvent {
	return models.AuditEvent{
		EventID:     "evt-" + action + "-" + resource,
		OccurredAt:  time.Now(),
		AgentID:     agentID,
		Action:      action,
		ResourceRef: resource,
		Attributes:  map[string]string{},
	}
}

func TestNewEngine(t *testing.T) {
	st := store.NewMemory(0)
	eng := NewEngine(st, testLogger())
	if eng == nil {
		t.Fatal("NewEngine returned nil")
	}
	if eng.alpha != 0.3 {
		t.Errorf("expected alpha 0.3, got %f", eng.alpha)
	}
}

func TestEngine_Evaluate_NoEvents(t *testing.T) {
	st := store.NewMemory(0)
	eng := NewEngine(st, testLogger())
	alerts := eng.Evaluate(context.Background(), nil)
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestEngine_Evaluate_SensitivePath_RaisesScore(t *testing.T) {
	st := store.NewMemory(0)
	// Seed agent via store
	st.UpsertAgent(context.Background(), models.Agent{
		ID: "test-agent", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	eng := NewEngine(st, testLogger())
	events := []models.AuditEvent{
		makeTestEvent("test-agent", "read", "/etc/passwd"),
	}

	alerts := eng.Evaluate(context.Background(), events)

	// SensitivePathRule returns 0.5, raw event score = 0.5
	// EMA: 0 + 0.3 * (0.5 - 0) = 0.15
	score := eng.GetAgentScore("test-agent")
	if score <= 0 {
		t.Errorf("expected score > 0 after sensitive path event, got %f", score)
	}
	_ = alerts
}

func TestEngine_Evaluate_RuleScores(t *testing.T) {
	st := store.NewMemory(0)
	st.UpsertAgent(context.Background(), models.Agent{
		ID: "agent-rules", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	eng := NewEngine(st, testLogger())

	tests := []struct {
		name     string
		action   string
		resource string
		wantMin  float64
	}{
		{"sensitive path", "read", "/etc/shadow", 0.5},
		{"risky write", "write", "/tmp/out.txt", 0.2},
		{"exec", "exec", "/usr/bin/python3", 0.2},
		{"normal read", "read", "/data/file.csv", 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events := []models.AuditEvent{makeTestEvent("agent-rules", tc.action, tc.resource)}
			eng.Evaluate(context.Background(), events)
			// Each call updates EMA starting from 0 with alpha 0.3
			score := eng.GetAgentScore("agent-rules")
			if tc.wantMin > 0 && score <= 0 {
				t.Errorf("expected score > 0 for %s, got %f", tc.name, score)
			}
			// Reset agent score for next test
			eng.mu.Lock()
			delete(eng.agentScores, "agent-rules")
			eng.mu.Unlock()
		})
	}
}

func TestEngine_Evaluate_EMA_Accumulates(t *testing.T) {
	st := store.NewMemory(0)
	st.UpsertAgent(context.Background(), models.Agent{
		ID: "ema-agent", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	eng := NewEngine(st, testLogger())

	// First event: /etc/passwd -> rule score 0.5
	// EMA: 0 + 0.3*(0.5) = 0.15
	eng.Evaluate(context.Background(), []models.AuditEvent{
		makeTestEvent("ema-agent", "read", "/etc/passwd"),
	})
	s1 := eng.GetAgentScore("ema-agent")

	// Second event: also /etc/passwd
	// EMA: 0.15 + 0.3*(0.5 - 0.15) = 0.15 + 0.105 = 0.255
	eng.Evaluate(context.Background(), []models.AuditEvent{
		makeTestEvent("ema-agent", "read", "/etc/passwd"),
	})
	s2 := eng.GetAgentScore("ema-agent")

	if s2 <= s1 {
		t.Errorf("EMA should accumulate: s1=%f s2=%f", s1, s2)
	}
}

func TestEngine_FormatScore(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.05, "0.05"},
		{0.999, "1.00"},
		{0.456, "0.46"},
	}

	for _, tc := range tests {
		got := formatScore(tc.score)
		if got != tc.want {
			t.Errorf("formatScore(%f) = %s, want %s", tc.score, got, tc.want)
		}
	}
}

func TestEngine_CheckThresholds(t *testing.T) {
	st := store.NewMemory(0)
	eng := NewEngine(st, testLogger())
	now := time.Now()
	ev := models.AuditEvent{
		EventID:       "evt-test",
		AgentID:       "agent-x",
		FamilyGroupID: "fg-1",
		ResourceRef:   "/etc/passwd",
		OccurredAt:    now,
		Action:        "read",
	}

	tests := []struct {
		ema         float64
		wantAlert   bool
		expectedSev string
	}{
		{0.00, false, ""},
		{0.10, false, ""},
		{0.29, false, ""},
		{0.30, true, "medium"},
		{0.45, true, "medium"},
		{0.59, true, "medium"},
		{0.60, true, "high"},
		{0.71, true, "high"},
		{0.79, true, "high"},
		{0.80, true, "critical"},
		{0.95, true, "critical"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("ema=%.2f", tc.ema), func(t *testing.T) {
			e := ev // copy per sub-test
			alert := eng.checkThresholds(&e, tc.ema)
			if !tc.wantAlert {
				if alert != nil {
					t.Errorf("EMA %.2f should NOT trigger alert, got %+v", tc.ema, alert)
				}
				return
			}
			if alert == nil {
				t.Fatalf("EMA %.2f should trigger alert", tc.ema)
			}
			if alert.Severity != tc.expectedSev {
				t.Errorf("EMA %.2f: expected severity %s, got %s", tc.ema, tc.expectedSev, alert.Severity)
			}
			if !strings.HasPrefix(alert.AlertID, "alert_") {
				t.Errorf("EMA %.2f: AlertID should start with 'alert_', got %s", tc.ema, alert.AlertID)
			}
			if alert.AgentID != ev.AgentID {
				t.Errorf("EMA %.2f: AgentID = %s, want %s", tc.ema, alert.AgentID, ev.AgentID)
			}
			if alert.FamilyGroupID != ev.FamilyGroupID {
				t.Errorf("EMA %.2f: FamilyGroupID = %s, want %s", tc.ema, alert.FamilyGroupID, ev.FamilyGroupID)
			}
			if !strings.Contains(alert.Title, ev.AgentID) {
				t.Errorf("EMA %.2f: Title should contain agent ID, got %s", tc.ema, alert.Title)
			}
			if !strings.Contains(alert.Description, formatScore(tc.ema)) {
				t.Errorf("EMA %.2f: Description should contain score '%s', got %s",
					tc.ema, formatScore(tc.ema), alert.Description)
			}
			if alert.Status != "open" {
				t.Errorf("EMA %.2f: Status = %s, want open", tc.ema, alert.Status)
			}
			if !alert.OccurredAt.Equal(now) {
				t.Errorf("EMA %.2f: OccurredAt = %v, want %v", tc.ema, alert.OccurredAt, now)
			}
		})
	}
}

func TestEngine_GetAgentScore_Unknown(t *testing.T) {
	st := store.NewMemory(0)
	eng := NewEngine(st, testLogger())
	score := eng.GetAgentScore("no-such-agent")
	if score != 0 {
		t.Errorf("expected 0 for unknown agent, got %f", score)
	}
}

func TestSetMLScorer(t *testing.T) {
	st := store.NewMemory(0)
	eng := NewEngine(st, testLogger())
	eng.SetMLScorer(nil)
	eng.mu.RLock()
	if eng.mlScorer != nil {
		t.Error("expected nil mlScorer after SetMLScorer(nil)")
	}
	eng.mu.RUnlock()
}
