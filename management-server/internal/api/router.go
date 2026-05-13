package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/policy"
	"agentshield.dev/agentshield/management-server/internal/risk"
	"agentshield.dev/agentshield/management-server/internal/store"
)

type Router struct {
	log  *slog.Logger
	store store.Store
	risk  *risk.Engine
	hub   *Hub
	pol   *policy.Distributor
}

func NewRouter(log *slog.Logger, s store.Store, re *risk.Engine, h *Hub, p *policy.Distributor) http.Handler {
	r := &Router{log: log, store: s, risk: re, hub: h, pol: p}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", r.healthz)

	// Agents
	m.HandleFunc("POST /api/v1/agents/register", r.registerAgent)
	m.HandleFunc("GET /api/v1/agents", r.listAgents)
	m.HandleFunc("GET /api/v1/agents/{id}", r.getAgent)
	m.HandleFunc("PUT /api/v1/agents/{id}/status", r.updateAgentStatus)

	// Audit
	m.HandleFunc("POST /api/v1/audit/events", r.appendAuditEvents)
	m.HandleFunc("GET /api/v1/audit/events", r.listAuditEvents)

	// Family Groups
	m.HandleFunc("GET /api/v1/family-groups", r.listFamilyGroups)
	m.HandleFunc("POST /api/v1/family-groups", r.createFamilyGroup)
	m.HandleFunc("GET /api/v1/family-groups/{id}", r.getFamilyGroup)
	m.HandleFunc("PUT /api/v1/family-groups/{id}", r.updateFamilyGroup)
	m.HandleFunc("DELETE /api/v1/family-groups/{id}", r.deleteFamilyGroup)

	// Policies
	m.HandleFunc("GET /api/v1/policies/bundles", r.listPolicyBundles)
	m.HandleFunc("POST /api/v1/policies/bundles", r.createPolicyBundle)
	m.HandleFunc("PUT /api/v1/policies/bundles/{version}/activate", r.activatePolicyBundle)

	// Alerts
	m.HandleFunc("GET /api/v1/alerts", r.listAlerts)
	m.HandleFunc("PUT /api/v1/alerts/{alertId}", r.updateAlert)

	// Dashboard
	m.HandleFunc("GET /api/v1/dashboard/stats", r.dashboardStats)

	// WebSocket
	m.HandleFunc("GET /api/v1/ws/events", r.hub.ServeHTTP)

	return m
}

// ── Health ──

func (r *Router) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": "0.1.0"})
}

// ── Family Groups ──

func (r *Router) listFamilyGroups(w http.ResponseWriter, req *http.Request) {
	groups, err := r.store.ListFamilyGroups(req.Context())
	if err != nil {
		r.log.Error("list family groups", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if groups == nil {
		groups = []models.FamilyGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (r *Router) createFamilyGroup(w http.ResponseWriter, req *http.Request) {
	var fg models.FamilyGroup
	if err := json.NewDecoder(req.Body).Decode(&fg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.store.CreateFamilyGroup(req.Context(), fg); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, fg)
}

func (r *Router) getFamilyGroup(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	fg, found, err := r.store.GetFamilyGroup(req.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, fg)
}

func (r *Router) updateFamilyGroup(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var fg models.FamilyGroup
	if err := json.NewDecoder(req.Body).Decode(&fg); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	fg.ID = id
	if err := r.store.UpdateFamilyGroup(req.Context(), fg); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fg)
}

func (r *Router) deleteFamilyGroup(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.store.DeleteFamilyGroup(req.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ── Agents ──

func (r *Router) registerAgent(w http.ResponseWriter, req *http.Request) {
	var body models.RegisterAgentRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	agent := models.Agent{
		ID:            body.ID,
		FamilyGroupID: body.FamilyGroupID,
		DisplayName:   body.DisplayName,
		Labels:        body.Labels,
		Status:        "online",
	}
	if err := r.store.UpsertAgent(req.Context(), agent); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a, _, _ := r.store.GetAgent(req.Context(), agent.ID)
	writeJSON(w, http.StatusOK, a)
}

func (r *Router) listAgents(w http.ResponseWriter, req *http.Request) {
	fgid := req.URL.Query().Get("family_group_id")
	status := req.URL.Query().Get("status")
	var list []models.Agent
	var err error
	if status != "" {
		list, err = r.store.ListAgentsByStatus(req.Context(), status)
	} else {
		list, err = r.store.ListAgents(req.Context(), fgid)
	}
	if err != nil {
		r.log.Error("list agents", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if list == nil {
		list = []models.Agent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": list})
}

func (r *Router) getAgent(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	a, found, err := r.store.GetAgent(req.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (r *Router) updateAgentStatus(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body models.UpdateAgentStatusRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.store.UpdateAgentStatus(req.Context(), id, body.Status); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	a, _, _ := r.store.GetAgent(req.Context(), id)
	writeJSON(w, http.StatusOK, a)
}

// ── Audit ──

func (r *Router) appendAuditEvents(w http.ResponseWriter, req *http.Request) {
	var payload struct {
		Events []models.AuditEvent `json:"events"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	// 风险评估
	alerts := r.risk.Evaluate(req.Context(), payload.Events)

	n, err := r.store.AppendAuditEvents(req.Context(), payload.Events)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// 持久化 agent 风险评分
	seen := map[string]bool{}
	for i := range payload.Events {
		agentID := payload.Events[i].AgentID
		if seen[agentID] {
			continue
		}
		seen[agentID] = true
		score := r.risk.GetAgentScore(agentID)
		if err := r.store.UpdateAgentRiskScore(req.Context(), agentID, score); err != nil {
			r.log.Debug("update risk score", "agent_id", agentID, "err", err)
		}
	}

	// 持久化告警
	for _, alert := range alerts {
		if err := r.store.CreateRiskAlert(req.Context(), alert); err != nil {
			r.log.Error("create alert", "err", err)
		}
	}

	// WebSocket 推送最近事件
	for i := len(payload.Events) - 1; i >= 0 && i >= len(payload.Events)-3; i-- {
		payloadBytes, _ := json.Marshal(payload.Events[i])
		r.hub.Broadcast(WSEvent{Type: "audit_event", Payload: payloadBytes})
	}
	for _, alert := range alerts {
		payloadBytes, _ := json.Marshal(alert)
		r.hub.Broadcast(WSEvent{Type: "risk_alert", Payload: payloadBytes})
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": n, "alerts_triggered": len(alerts)})
}

func (r *Router) listAuditEvents(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	filter := models.AuditEventFilter{
		AgentID:       q.Get("agent_id"),
		FamilyGroupID: q.Get("family_group_id"),
		Action:        q.Get("action"),
		Limit:         50,
		Offset:        0,
	}
	if l := q.Get("limit"); l != "" {
		filter.Limit = parseInt(l, 50)
	}
	if o := q.Get("offset"); o != "" {
		filter.Offset = parseInt(o, 0)
	}

	// 使用过滤查询
	events, total, err := r.store.ListAuditEventsFiltered(req.Context(), filter)
	if err != nil {
		r.log.Error("list audit events", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if events == nil {
		events = []models.AuditEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "total": total})
}

// ── Policies ──

func (r *Router) listPolicyBundles(w http.ResponseWriter, req *http.Request) {
	bundles, err := r.store.ListPolicyBundles(req.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if bundles == nil {
		bundles = []models.PolicyBundle{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundles": bundles})
}

func (r *Router) createPolicyBundle(w http.ResponseWriter, req *http.Request) {
	var pb models.PolicyBundle
	if err := json.NewDecoder(req.Body).Decode(&pb); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.store.CreatePolicyBundle(req.Context(), pb); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pb)
}

func (r *Router) activatePolicyBundle(w http.ResponseWriter, req *http.Request) {
	version := req.PathValue("version")
	if err := r.pol.ActivatePolicy(req.Context(), version); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"activated": version})
}

// ── Alerts ──

func (r *Router) listAlerts(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	filter := models.AlertFilter{
		FamilyGroupID: q.Get("family_group_id"),
		Severity:      q.Get("severity"),
		Status:        q.Get("status"),
		Limit:         50,
		Offset:        0,
	}
	if l := q.Get("limit"); l != "" {
		filter.Limit = parseInt(l, 50)
	}
	if o := q.Get("offset"); o != "" {
		filter.Offset = parseInt(o, 0)
	}
	alerts, total, err := r.store.ListRiskAlerts(req.Context(), filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if alerts == nil {
		alerts = []models.RiskAlert{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts, "total": total})
}

func (r *Router) updateAlert(w http.ResponseWriter, req *http.Request) {
	alertID := req.PathValue("alertId")
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.store.UpdateRiskAlertStatus(req.Context(), alertID, body.Status); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// WebSocket 推送状态更新
	alertBytes, _ := json.Marshal(map[string]string{"alert_id": alertID, "status": body.Status})
	r.hub.Broadcast(WSEvent{Type: "alert_update", Payload: alertBytes})

	writeJSON(w, http.StatusOK, map[string]string{"alert_id": alertID, "status": body.Status})
}

// ── Dashboard ──

func (r *Router) dashboardStats(w http.ResponseWriter, req *http.Request) {
	fgid := req.URL.Query().Get("family_group_id")
	stats, err := r.store.GetDashboardStats(req.Context(), fgid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
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

func parseInt(s string, def int) int {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}
