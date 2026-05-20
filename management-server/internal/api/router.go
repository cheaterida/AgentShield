package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/policy"
	"agentshield.dev/agentshield/management-server/internal/risk"
	"agentshield.dev/agentshield/management-server/internal/store"
)

type Router struct {
	log       *slog.Logger
	store     store.Store
	risk      *risk.Engine
	hub       *Hub
	pol       *policy.Distributor
	opaClient *policy.OPAClient
}

func NewRouter(log *slog.Logger, s store.Store, re *risk.Engine, h *Hub, p *policy.Distributor, opa *policy.OPAClient) http.Handler {
	r := &Router{log: log, store: s, risk: re, hub: h, pol: p, opaClient: opa}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", r.healthz)
	m.HandleFunc("GET /api/v1/healthz", r.healthz)

	// Agents
	m.HandleFunc("POST /api/v1/agents/register", r.registerAgent)
	m.HandleFunc("POST /api/v1/agents/heartbeat", r.agentHeartbeat)
	m.HandleFunc("GET /api/v1/agents", r.listAgents)
	m.HandleFunc("GET /api/v1/agents/{id}", r.getAgent)
	m.HandleFunc("PUT /api/v1/agents/{id}/status", r.updateAgentStatus)

	// Audit
	m.HandleFunc("POST /api/v1/audit/events", r.appendAuditEvents)
	m.HandleFunc("GET /api/v1/audit/events", r.listAuditEvents)
	m.HandleFunc("POST /api/v1/spans", r.ingestSpans)

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

func (r *Router) agentHeartbeat(w http.ResponseWriter, req *http.Request) {
	var hb models.AgentHeartbeat
	if err := json.NewDecoder(req.Body).Decode(&hb); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.store.UpdateAgentHeartbeat(req.Context(), hb.AgentID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := models.HeartbeatResponse{
		Acknowledged:        true,
		LatestPolicyVersion: r.pol.GetCurrentVersion(req.Context()),
		SuggestedAction:     r.computeSuggestedAction(hb.AgentID),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) computeSuggestedAction(agentID string) string {
	score := r.risk.GetAgentScore(agentID)
	if score >= 0.8 {
		return "isolate"
	}
	if score >= 0.6 {
		return "restart_probe"
	}
	return "ok"
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

	// OPA 策略评估
	opaAlerts := r.evaluateOPA(req.Context(), payload.Events)
	alerts = append(alerts, opaAlerts...)

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

// ── Spans (from agentshield_tracer.py) ──

func (r *Router) ingestSpans(w http.ResponseWriter, req *http.Request) {
	agentID := req.Header.Get("X-AgentShield-Agent-ID")
	familyGroupID := req.Header.Get("X-AgentShield-Family-Group-ID")
	if agentID == "" {
		agentID = "unknown"
	}
	if familyGroupID == "" {
		familyGroupID = "default"
	}

	var payload struct {
		Spans []SpanIngest `json:"spans"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	var events []models.AuditEvent
	for _, sp := range payload.Spans {
		events = append(events, sp.toAuditEvent(agentID, familyGroupID))
	}

	if len(events) > 0 {
		alerts := r.risk.Evaluate(req.Context(), events)
		n, err := r.store.AppendAuditEvents(req.Context(), events)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		for _, alert := range alerts {
			if err := r.store.CreateRiskAlert(req.Context(), alert); err != nil {
				r.log.Error("create alert", "err", err)
			}
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": n, "alerts_triggered": len(alerts)})
	} else {
		writeJSON(w, http.StatusAccepted, map[string]any{"accepted": 0})
	}
}

type SpanIngest struct {
	TraceID  string            `json:"trace_id"`
	SpanID   string            `json:"span_id"`
	Name     string            `json:"name"`
	Kind     int               `json:"kind"`
	Start    string            `json:"start_time"`
	End      string            `json:"end_time"`
	Duration int64             `json:"duration"`
	Attrs    map[string]string `json:"attributes"`
}

var spanTimeFormats = []string{
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05.000Z07:00",
	"2006-01-02T15:04:05Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
}

func (s SpanIngest) toAuditEvent(agentID, familyGroupID string) models.AuditEvent {
	action := "llm.call"
	if s.Attrs != nil {
		if op, ok := s.Attrs["gen_ai.operation.name"]; ok {
			action = "llm." + op
		}
	}
	resource := s.Name
	if s.Attrs != nil {
		if model, ok := s.Attrs["gen_ai.request.model"]; ok {
			resource = model
		}
	}
	var ts time.Time
	for _, f := range spanTimeFormats {
		if t, err := time.Parse(f, s.Start); err == nil {
			ts = t
			break
		}
	}
	attrs := map[string]string{
		"span_id":  s.SpanID,
		"trace_id": s.TraceID,
		"duration": fmt.Sprintf("%d", s.Duration),
	}
	if s.Attrs != nil {
		for k, v := range s.Attrs {
			attrs[k] = v
		}
	}
	return models.AuditEvent{
		EventID:       s.SpanID,
		OccurredAt:    ts,
		FamilyGroupID: familyGroupID,
		AgentID:       agentID,
		ResourceRef:   resource,
		Action:        action,
		Attributes:    attrs,
	}
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

// evaluateOPA 对每个审计事件调用 OPA 策略评估。
// 返回因策略违规触发的告警列表。
func (r *Router) evaluateOPA(ctx context.Context, events []models.AuditEvent) []models.RiskAlert {
	if r.opaClient == nil {
		return nil
	}

	var policyAlerts []models.RiskAlert

	for i := range events {
		ev := &events[i]

		// 构建 OPA 输入
		destination := ""
		if ev.Attributes != nil {
			if d, ok := ev.Attributes["network_dst"]; ok {
				destination = d
			}
		}
		opaInput := map[string]any{
			"subject":       map[string]string{"family_group_id": ev.FamilyGroupID},
			"action":        ev.Action,
			"resource_ref":  ev.ResourceRef,
			"destination":   destination,
			"risk_score":    ev.RiskContribution,
		}

		result, err := r.opaClient.Evaluate(ctx, "agentshield/audit", opaInput)
		if err != nil {
			r.log.Debug("opa evaluate error", "event_id", ev.EventID, "err", err)
			continue
		}

		// 将 OPA 决策写入事件属性
		if ev.Attributes == nil {
			ev.Attributes = make(map[string]string)
		}
		allowed, _ := result["allow"].(bool)
		denyPath, _ := result["deny_sensitive_path"].(bool)
		denyNet, _ := result["deny_network"].(bool)
		riskyWrite, _ := result["risky_write"].(bool)
		riskLevel, _ := result["risk_level"].(string)
		matchedPath, _ := result["matched_path"].(string)

		if allowed {
			ev.Attributes["opa_allow"] = "true"
		} else {
			ev.Attributes["opa_allow"] = "false"
		}
		if denyPath {
			ev.Attributes["opa_deny_sensitive_path"] = "true"
			ev.Attributes["opa_matched_path"] = matchedPath
		}
		if denyNet {
			ev.Attributes["opa_deny_network"] = "true"
		}
		if riskyWrite {
			ev.Attributes["opa_risky_write"] = "true"
		}
		if riskLevel != "" {
			ev.Attributes["opa_risk_level"] = riskLevel
		}

		// 如果策略拒绝或触发高风险规则，生成告警
		if denyPath || denyNet || !allowed {
			severity := "high"
			if riskLevel == "critical" {
				severity = "critical"
			}
			title := "OPA policy violation"
			desc := ""
			if denyPath {
				title = "Sensitive path access blocked"
				desc = "Agent " + ev.AgentID + " attempted to access " + matchedPath
			} else if denyNet {
				title = "Restricted network access blocked"
				desc = "Agent " + ev.AgentID + " attempted network connection to " + destination
			} else if !allowed {
				title = "Unauthorized action blocked"
				desc = "Agent " + ev.AgentID + " attempted disallowed action: " + ev.Action
			}

			policyAlerts = append(policyAlerts, models.RiskAlert{
				AlertID:       "opa_" + ev.EventID,
				FamilyGroupID: ev.FamilyGroupID,
				AgentID:       ev.AgentID,
				Severity:      severity,
				Title:         title,
				Description:   desc,
				Status:        "open",
				OccurredAt:    ev.OccurredAt,
			})
		}
	}
	return policyAlerts
}

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
