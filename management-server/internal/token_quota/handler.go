package token_quota

import (
	"encoding/json"
	"net/http"
	"strconv"

	"agentshield.dev/agentshield/management-server/internal/models"
)

// ── Agent Usage ──

func (m *Manager) HandleAgentUsage(w http.ResponseWriter, req *http.Request) {
	agentID := req.PathValue("id")
	usage, err := m.GetAgentUsage(req.Context(), agentID)
	if err != nil {
		m.log.Error("get agent usage", "agent_id", agentID, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// ── FamilyGroup Usage ──

func (m *Manager) HandleFamilyGroupUsage(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	summary, err := m.store.GetTokenUsageSummary(req.Context(), "family_group", id, "daily")
	if err != nil {
		m.log.Error("get family group usage", "id", id, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summaries": summary})
}

// ── Usage Summary ──

func (m *Manager) HandleUsageSummary(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	targetType := q.Get("target_type")
	targetID := q.Get("target_id")
	period := q.Get("period")
	if period == "" {
		period = "daily"
	}

	var summaries []models.TokenUsageSummary
	var err error
	if targetType != "" && targetID != "" {
		summaries, err = m.store.GetTokenUsageSummary(req.Context(), targetType, targetID, period)
	} else {
		// 全局摘要：遍历所有 agent 的 daily 汇总
		summaries, err = m.store.GetTokenUsageSummary(req.Context(), "agent", "", period)
	}
	if err != nil {
		m.log.Error("get usage summary", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"summaries": summaries})
}

// ── Usage Logs ──

func (m *Manager) HandleUsageLogs(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	filter := models.TokenUsageLogFilter{
		AgentID:       q.Get("agent_id"),
		FamilyGroupID: q.Get("family_group_id"),
		ModelName:     q.Get("model"),
		FromTime:      q.Get("from"),
		ToTime:        q.Get("to"),
		Limit:         50,
		Offset:        0,
	}
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			filter.Limit = n
		}
	}
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil {
			filter.Offset = n
		}
	}
	logs, total, err := m.store.GetTokenUsageLogs(req.Context(), filter)
	if err != nil {
		m.log.Error("get usage logs", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if logs == nil {
		logs = []models.TokenUsageLog{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total})
}

// ── Model Prices ──

func (m *Manager) HandleListPrices(w http.ResponseWriter, req *http.Request) {
	prices, err := m.store.ListModelPrices(req.Context())
	if err != nil {
		m.log.Error("list model prices", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if prices == nil {
		prices = []models.ModelPrice{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": prices})
}

func (m *Manager) HandleUpsertPrice(w http.ResponseWriter, req *http.Request) {
	var p models.ModelPrice
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if p.ModelID == "" {
		writeErr(w, http.StatusBadRequest, "model_id required")
		return
	}
	if err := m.store.UpsertModelPrice(req.Context(), p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// ── Quota CRUD ──

func (m *Manager) HandleListQuotas(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	targetType := q.Get("target_type")
	quotas, err := m.store.ListTokenQuotas(req.Context(), targetType)
	if err != nil {
		m.log.Error("list quotas", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if quotas == nil {
		quotas = []models.TokenQuota{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"quotas": quotas})
}

func (m *Manager) HandleCreateQuota(w http.ResponseWriter, req *http.Request) {
	var q models.TokenQuota
	if err := json.NewDecoder(req.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if q.QuotaID == "" || q.TargetType == "" || q.TargetID == "" {
		writeErr(w, http.StatusBadRequest, "quota_id, target_type, target_id required")
		return
	}
	if err := m.store.CreateTokenQuota(req.Context(), q); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

func (m *Manager) HandleUpdateQuota(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var q models.TokenQuota
	if err := json.NewDecoder(req.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	q.QuotaID = id
	if err := m.store.UpdateTokenQuota(req.Context(), q); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (m *Manager) HandleDeleteQuota(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := m.store.DeleteTokenQuota(req.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── helpers ──

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
