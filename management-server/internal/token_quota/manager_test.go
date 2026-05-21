package token_quota

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	st := store.NewMemory(10000)
	m := New(st, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))
	// seed model prices
	st.UpsertModelPrice(context.Background(), models.ModelPrice{
		ModelID: "gpt-4o", Provider: "openai", DisplayName: "GPT-4o",
		InputPriceMillicents: 250000, OutputPriceMillicents: 1000000, Active: true,
	})
	return m
}

func TestExtractTokenUsage(t *testing.T) {
	tests := []struct {
		name          string
		attrs         map[string]string
		wantModel     string
		wantInput     int64
		wantOutput    int64
		wantTotal     int64
	}{
		{
			name: "complete token data",
			attrs: map[string]string{
				"gen_ai.response.model":   "gpt-4o",
				"gen_ai.usage.input_tokens":  "100",
				"gen_ai.usage.output_tokens": "50",
				"gen_ai.usage.total_tokens":  "150",
			},
			wantModel: "gpt-4o", wantInput: 100, wantOutput: 50, wantTotal: 150,
		},
		{
			name: "no total — fallback to input+output",
			attrs: map[string]string{
				"gen_ai.request.model":    "gpt-4o",
				"gen_ai.usage.input_tokens":  "200",
				"gen_ai.usage.output_tokens": "100",
			},
			wantModel: "gpt-4o", wantInput: 200, wantOutput: 100, wantTotal: 300,
		},
		{
			name:      "empty attrs",
			attrs:     map[string]string{},
			wantModel: "", wantInput: 0, wantOutput: 0, wantTotal: 0,
		},
		{
			name: "response model takes precedence",
			attrs: map[string]string{
				"gen_ai.request.model":    "gpt-4o-mini",
				"gen_ai.response.model":   "gpt-4o",
				"gen_ai.usage.input_tokens":  "10",
			},
			wantModel: "gpt-4o", wantInput: 10, wantOutput: 0, wantTotal: 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, input, output, total := extractTokenUsage(tt.attrs)
			if model != tt.wantModel || input != tt.wantInput || output != tt.wantOutput || total != tt.wantTotal {
				t.Errorf("extractTokenUsage() = (%q, %d, %d, %d), want (%q, %d, %d, %d)",
					model, input, output, total, tt.wantModel, tt.wantInput, tt.wantOutput, tt.wantTotal)
			}
		})
	}
}

func TestCalculateCost(t *testing.T) {
	prices := []models.ModelPrice{
		{ModelID: "gpt-4o", InputPriceMillicents: 250000, OutputPriceMillicents: 1000000, Active: true},
		{ModelID: "gpt-4o-mini", InputPriceMillicents: 15000, OutputPriceMillicents: 60000, Active: true},
	}
	tests := []struct {
		name     string
		model    string
		input    int64
		output   int64
		wantCost int64
	}{
		{name: "gpt-4o 1M input", model: "gpt-4o", input: 1000000, output: 0, wantCost: 250000},
		{name: "gpt-4o 1M output", model: "gpt-4o", input: 0, output: 1000000, wantCost: 1000000},
		{name: "gpt-4o-mini 100 input + 50 output", model: "gpt-4o-mini", input: 100, output: 50,
			wantCost: (100 * 15000 / 1000000) + (50 * 60000 / 1000000)},
		{name: "unknown model", model: "unknown", input: 1000, output: 500, wantCost: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateCost(tt.model, tt.input, tt.output, prices); got != tt.wantCost {
				t.Errorf("calculateCost() = %d, want %d", got, tt.wantCost)
			}
		})
	}
}

func TestRecordUsageAndCheckQuota(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	// Create a quota for the agent
	st := m.store.(*store.Memory)
	st.CreateTokenQuota(ctx, models.TokenQuota{
		QuotaID: "q1", TargetType: "agent", TargetID: "agent-001",
		DailyLimit: 1000, WarnThreshold: 0.8, BlockThreshold: 1.0, Active: true,
	})

	// Record usage — 500 tokens
	alert := m.RecordUsage(ctx, "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "400",
		"gen_ai.usage.output_tokens": "100",
		"gen_ai.usage.total_tokens":  "500",
		"span_id":                    "span1",
	})
	if alert != nil {
		t.Fatalf("unexpected alert after 500/1000 usage: %+v", alert)
	}

	// Record usage again — 400 tokens (total 900 = 90%, should warn)
	alert = m.RecordUsage(ctx, "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "300",
		"gen_ai.usage.output_tokens": "100",
		"gen_ai.usage.total_tokens":  "400",
		"span_id":                    "span2",
	})
	if alert == nil {
		t.Fatal("expected warning alert at 90% usage")
	}
	if alert.Severity != "info" {
		t.Errorf("expected info severity for warn, got %s", alert.Severity)
	}

	// Record usage — 200 tokens (total 1100 = 110%, should block)
	alert = m.RecordUsage(ctx, "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "150",
		"gen_ai.usage.output_tokens": "50",
		"gen_ai.usage.total_tokens":  "200",
		"span_id":                    "span3",
	})
	if alert == nil {
		t.Fatal("expected blocked alert at 110% usage")
	}
	if alert.Severity != "medium" {
		t.Errorf("expected medium severity for block, got %s", alert.Severity)
	}

	// CheckQuota should reflect blocked status
	status, used, limit := m.CheckQuota(ctx, "agent-001")
	if status != "blocked" {
		t.Errorf("expected blocked status, got %s", status)
	}
	if used != 1100 {
		t.Errorf("expected used=1100, got %d", used)
	}
	if limit != 1000 {
		t.Errorf("expected limit=1000, got %d", limit)
	}
}

func TestFamilyGroupFallback(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	// Only family_group quota exists, no agent-level quota
	st := m.store.(*store.Memory)
	st.CreateTokenQuota(ctx, models.TokenQuota{
		QuotaID: "q1", TargetType: "family_group", TargetID: "fg1",
		DailyLimit: 5000, WarnThreshold: 0.8, BlockThreshold: 1.0, Active: true,
	})

	// Agent has no individual quota — should fallback to family_group
	alert := m.RecordUsage(ctx, "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "3000",
		"gen_ai.usage.output_tokens": "2000",
		"gen_ai.usage.total_tokens":  "5000",
		"span_id":                    "span1",
	})
	if alert == nil {
		t.Fatal("expected blocked alert — family group quota exceeded")
	}
	if alert.Severity != "medium" {
		t.Errorf("expected medium severity, got %s", alert.Severity)
	}
}

func TestRecordUsageNoQuota(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	// No quota defined — should not crash, no alert
	alert := m.RecordUsage(ctx, "agent-no-quota", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "100",
		"gen_ai.usage.output_tokens": "50",
		"span_id":                    "span1",
	})
	if alert != nil {
		t.Errorf("expected no alert when no quota defined, got %+v", alert)
	}

	// But usage should still be logged
	status, used, limit := m.CheckQuota(ctx, "agent-no-quota")
	if status != "no_quota" {
		t.Errorf("expected no_quota, got %s", status)
	}
	if used != 0 || limit != 0 {
		t.Errorf("expected used=0, limit=0, got %d, %d", used, limit)
	}
}

func TestGetAgentUsage(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	usage, err := m.GetAgentUsage(ctx, "agent-001")
	if err != nil {
		t.Fatalf("GetAgentUsage: %v", err)
	}
	if usage["daily_total_tokens"] != 0 {
		t.Errorf("expected 0 daily usage for new agent, got %d", usage["daily_total_tokens"])
	}

	// Record some usage
	m.RecordUsage(ctx, "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "100",
		"gen_ai.usage.output_tokens": "50",
		"gen_ai.usage.total_tokens":  "150",
		"span_id":                    "span1",
	})

	usage, err = m.GetAgentUsage(ctx, "agent-001")
	if err != nil {
		t.Fatalf("GetAgentUsage: %v", err)
	}
	if usage["daily_total_tokens"] != 150 {
		t.Errorf("expected daily_total_tokens=150, got %d", usage["daily_total_tokens"])
	}
	if usage["total_total_tokens"] != 150 {
		t.Errorf("expected total_total_tokens=150, got %d", usage["total_total_tokens"])
	}
}
