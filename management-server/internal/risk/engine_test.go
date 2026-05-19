package risk

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func makeTestEvent(action, resource string) models.AuditEvent {
	return models.AuditEvent{
		EventID:     "evt-" + action + "-" + resource,
		OccurredAt:  time.Now(),
		AgentID:     "test-agent",
		Action:      action,
		ResourceRef: resource,
		Attributes:  map[string]string{},
	}
}

func TestNewEngine(t *testing.T) {
	st := store.NewMemory()
	eng := NewEngine(st, testLogger())
	if eng == nil {
		t.Fatal("NewEngine returned nil")
	}
	if eng.alpha != 0.3 {
		t.Errorf("expected alpha 0.3, got %f", eng.alpha)
	}
}

func TestEngine_Evaluate_NoEvents(t *testing.T) {
	st := store.NewMemory()
	eng := NewEngine(st, testLogger())
	alerts := eng.Evaluate(context.Background(), nil)
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestEngine_Evaluate_SensitivePath_RaisesScore(t *testing.T) {
	st := store.NewMemory()
	// Seed agent via store
	st.UpsertAgent(context.Background(), models.Agent{
		ID: "test-agent", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	eng := NewEngine(st, testLogger())
	events := []models.AuditEvent{
		makeTestEvent("read", "/etc/passwd"),
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
	st := store.NewMemory()
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
			events := []models.AuditEvent{makeTestEvent(tc.action, tc.resource)}
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
	st := store.NewMemory()
	st.UpsertAgent(context.Background(), models.Agent{
		ID: "ema-agent", FamilyGroupID: "fg", Status: "online",
		RegisteredAt: time.Now(), UpdatedAt: time.Now(),
	})

	eng := NewEngine(st, testLogger())

	// First event: /etc/passwd → rule score 0.5
	// EMA: 0 + 0.3*(0.5) = 0.15
	eng.Evaluate(context.Background(), []models.AuditEvent{
		makeTestEvent("read", "/etc/passwd"),
	})
	s1 := eng.GetAgentScore("ema-agent")

	// Second event: also /etc/passwd
	// EMA: 0.15 + 0.3*(0.5 - 0.15) = 0.15 + 0.105 = 0.255
	eng.Evaluate(context.Background(), []models.AuditEvent{
		makeTestEvent("read", "/etc/passwd"),
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
	st := store.NewMemory()
	eng := NewEngine(st, testLogger())

	tests := []struct {
		ema             float64
		wantAlert       bool
		expectedSev     string
	}{
		{0.10, false, ""},
		{0.25, false, ""},
		{0.30, true, "medium"},
		{0.55, true, "medium"},
		{0.60, true, "high"},
		{0.75, true, "high"},
		{0.80, true, "critical"},
		{0.95, true, "critical"},
	}

	for _, tc := range tests {
		t.Run(tc.expectedSev, func(t *testing.T) {
			alerts := eng.checkThresholds("agent-x", "fg", tc.ema)
			if tc.wantAlert && len(alerts) == 0 {
				t.Errorf("EMA %f should trigger alert", tc.ema)
			}
			if !tc.wantAlert && len(alerts) > 0 {
				t.Errorf("EMA %f should NOT trigger alert, got %v", tc.ema, alerts)
			}
			for _, a := range alerts {
				if a.Severity != tc.expectedSev {
					t.Errorf("EMA %f: expected severity %s, got %s", tc.ema, tc.expectedSev, a.Severity)
				}
			}
		})
	}
}

func TestEngine_GetAgentScore_Unknown(t *testing.T) {
	st := store.NewMemory()
	eng := NewEngine(st, testLogger())
	score := eng.GetAgentScore("no-such-agent")
	if score != 0 {
		t.Errorf("expected 0 for unknown agent, got %f", score)
	}
}

func TestSetMLScorer(t *testing.T) {
	st := store.NewMemory()
	eng := NewEngine(st, testLogger())
	eng.SetMLScorer(nil)
	eng.mu.RLock()
	if eng.mlScorer != nil {
		t.Error("expected nil mlScorer after SetMLScorer(nil)")
	}
	eng.mu.RUnlock()
}
