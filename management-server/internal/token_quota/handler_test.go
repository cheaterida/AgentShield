package token_quota

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

func newTestHandler(t *testing.T) *Manager {
	t.Helper()
	st := store.NewMemory(10000)
	m := New(st, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))
	// seed prices
	st.UpsertModelPrice(context.Background(), models.ModelPrice{
		ModelID: "gpt-4o", Provider: "openai", DisplayName: "GPT-4o",
		InputPriceMillicents: 250000, OutputPriceMillicents: 1000000, Active: true,
	})
	return m
}

func TestHandleAgentUsage(t *testing.T) {
	m := newTestHandler(t)
	// Seed some usage
	m.RecordUsage(context.Background(), "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "100",
		"gen_ai.usage.output_tokens": "50",
		"span_id":                    "span1",
	})

	req := httptest.NewRequest("GET", "/api/v1/quota/agents/agent-001/usage", nil)
	req.SetPathValue("id", "agent-001")
	w := httptest.NewRecorder()
	m.HandleAgentUsage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]int64
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if body["daily_total_tokens"] != 150 {
		t.Errorf("expected daily_total_tokens=150, got %d", body["daily_total_tokens"])
	}
}

func TestHandleListPrices(t *testing.T) {
	m := newTestHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/quota/prices", nil)
	w := httptest.NewRecorder()
	m.HandleListPrices(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Prices []models.ModelPrice `json:"prices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(resp.Prices) == 0 {
		t.Error("expected at least 1 price")
	}
}

func TestHandleUpsertPrice(t *testing.T) {
	m := newTestHandler(t)
	body := `{"model_id":"test-model","provider":"test","input_price_millicents":100,"output_price_millicents":200}`
	req := httptest.NewRequest("POST", "/api/v1/quota/prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.HandleUpsertPrice(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's listed
	listReq := httptest.NewRequest("GET", "/api/v1/quota/prices", nil)
	listW := httptest.NewRecorder()
	m.HandleListPrices(listW, listReq)
	var resp struct {
		Prices []models.ModelPrice `json:"prices"`
	}
	json.Unmarshal(listW.Body.Bytes(), &resp)
	found := false
	for _, p := range resp.Prices {
		if p.ModelID == "test-model" {
			found = true
			break
		}
	}
	if !found {
		t.Error("test-model not found in price list after upsert")
	}
}

func TestHandleQuotaCRUD(t *testing.T) {
	m := newTestHandler(t)

	// Create
	createBody := `{"quota_id":"q1","target_type":"agent","target_id":"agent-001","daily_limit":1000,"monthly_limit":5000}`
	req := httptest.NewRequest("POST", "/api/v1/quota/quotas", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.HandleCreateQuota(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// List
	listReq := httptest.NewRequest("GET", "/api/v1/quota/quotas", nil)
	listW := httptest.NewRecorder()
	m.HandleListQuotas(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", listW.Code)
	}
	var listResp struct {
		Quotas []models.TokenQuota `json:"quotas"`
	}
	json.Unmarshal(listW.Body.Bytes(), &listResp)
	if len(listResp.Quotas) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(listResp.Quotas))
	}

	// Update
	updateBody := `{"target_type":"agent","target_id":"agent-001","daily_limit":2000,"monthly_limit":10000}`
	updateReq := httptest.NewRequest("PUT", "/api/v1/quota/quotas/q1", strings.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.SetPathValue("id", "q1")
	updateW := httptest.NewRecorder()
	m.HandleUpdateQuota(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	// Delete
	delReq := httptest.NewRequest("DELETE", "/api/v1/quota/quotas/q1", nil)
	delReq.SetPathValue("id", "q1")
	delW := httptest.NewRecorder()
	m.HandleDeleteQuota(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", delW.Code)
	}
}

func TestHandleUsageLogs(t *testing.T) {
	m := newTestHandler(t)

	// Record some usage so we have logs
	m.RecordUsage(context.Background(), "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "100",
		"gen_ai.usage.output_tokens": "50",
		"span_id":                    "span1",
	})

	req := httptest.NewRequest("GET", "/api/v1/quota/logs?agent_id=agent-001", nil)
	w := httptest.NewRecorder()
	m.HandleUsageLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Logs  []models.TokenUsageLog `json:"logs"`
		Total int                    `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("expected total=1, got %d", resp.Total)
	}
	if len(resp.Logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(resp.Logs))
	}
}

func TestHandleCreateQuotaValidation(t *testing.T) {
	m := newTestHandler(t)

	// Missing required fields
	body := `{"daily_limit":1000}`
	req := httptest.NewRequest("POST", "/api/v1/quota/quotas", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.HandleCreateQuota(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing fields, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteQuotaNotFound(t *testing.T) {
	m := newTestHandler(t)
	req := httptest.NewRequest("DELETE", "/api/v1/quota/quotas/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	m.HandleDeleteQuota(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleFamilyGroupUsage(t *testing.T) {
	m := newTestHandler(t)
	// Seed summary
	m.store.UpsertTokenUsageSummary(context.Background(), models.TokenUsageSummary{
		TargetType: "family_group", TargetID: "fg1",
		Period: "daily", DateKey: "2026-05-20",
		TotalTokens: 500, RequestCount: 5, CostMillicents: 250000,
	})

	req := httptest.NewRequest("GET", "/api/v1/quota/family-groups/fg1/usage", nil)
	req.SetPathValue("id", "fg1")
	w := httptest.NewRecorder()
	m.HandleFamilyGroupUsage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleUsageSummary(t *testing.T) {
	m := newTestHandler(t)
	// Seed agent usage
	m.RecordUsage(context.Background(), "agent-001", "fg1", map[string]string{
		"gen_ai.response.model":     "gpt-4o",
		"gen_ai.usage.input_tokens":  "200",
		"gen_ai.usage.output_tokens": "100",
		"span_id":                    "span1",
	})

	req := httptest.NewRequest("GET", "/api/v1/quota/usage/summary?target_type=agent&target_id=agent-001&period=daily", nil)
	w := httptest.NewRecorder()
	m.HandleUsageSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Summaries []models.TokenUsageSummary `json:"summaries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(resp.Summaries) == 0 {
		t.Error("expected at least 1 summary")
	}
}

func TestHandleUpsertPriceMissingModel(t *testing.T) {
	m := newTestHandler(t)
	body := `{"provider":"test"}`
	req := httptest.NewRequest("POST", "/api/v1/quota/prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	m.HandleUpsertPrice(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleUsageLogsEmptyResults(t *testing.T) {
	m := newTestHandler(t)
	req := httptest.NewRequest("GET", "/api/v1/quota/logs?agent_id=nonexistent", nil)
	w := httptest.NewRecorder()
	m.HandleUsageLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Logs  []models.TokenUsageLog `json:"logs"`
		Total int                    `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 0 {
		t.Errorf("expected total=0, got %d", resp.Total)
	}
	if len(resp.Logs) != 0 {
		t.Errorf("expected 0 logs, got %d", len(resp.Logs))
	}
}
