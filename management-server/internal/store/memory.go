package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
)

var ErrNotFound = errors.New("not found")

// DebugLog is an optional package-level logger for debug tracing store operations.
// Set via SetLogger; when nil (default), debug logging is skipped entirely.
var DebugLog *slog.Logger

// SetLogger configures the package-level debug logger for store operations.
func SetLogger(l *slog.Logger) {
	DebugLog = l
}

func debugLog(msg string, args ...any) {
	if DebugLog != nil {
		DebugLog.Debug(msg, args...)
	}
}

type Memory struct {
	mu           sync.RWMutex
	agents       map[string]models.Agent
	familyGroups map[string]models.FamilyGroup
	auditEvents  []models.AuditEvent
	policies     []models.PolicyBundle
	alerts       []models.RiskAlert
	auditCap     int

	tokenQuotas     map[string]models.TokenQuota
	tokenUsageLogs  []models.TokenUsageLog
	usageSummaries  []models.TokenUsageSummary
	modelPrices     map[string]models.ModelPrice
}

func NewMemory(auditCap int) *Memory {
	if auditCap <= 0 {
		auditCap = 10000
	}
	return &Memory{
		agents:          make(map[string]models.Agent),
		familyGroups:    make(map[string]models.FamilyGroup),
		auditCap:        auditCap,
		tokenQuotas:     make(map[string]models.TokenQuota),
		modelPrices:     make(map[string]models.ModelPrice),
	}
}

// ── FamilyGroup ──

func (m *Memory) CreateFamilyGroup(_ context.Context, fg models.FamilyGroup) error {
	if fg.ID == "" {
		return errors.New("family_group id required")
	}
	now := time.Now().UTC()
	fg.CreatedAt = now
	fg.UpdatedAt = now
	m.mu.Lock()
	m.familyGroups[fg.ID] = fg
	m.mu.Unlock()
	return nil
}

func (m *Memory) GetFamilyGroup(_ context.Context, id string) (models.FamilyGroup, bool, error) {
	m.mu.RLock()
	fg, ok := m.familyGroups[id]
	m.mu.RUnlock()
	if !ok {
		debugLog("GetFamilyGroup not found", "id", id, "store", "memory")
	}
	return fg, ok, nil
}

func (m *Memory) ListFamilyGroups(_ context.Context) ([]models.FamilyGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.FamilyGroup, 0, len(m.familyGroups))
	for _, fg := range m.familyGroups {
		out = append(out, fg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) UpdateFamilyGroup(_ context.Context, fg models.FamilyGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.familyGroups[fg.ID]
	if !ok {
		return ErrNotFound
	}
	existing.DisplayName = fg.DisplayName
	existing.Labels = fg.Labels
	existing.MemberPrincipalIDs = fg.MemberPrincipalIDs
	existing.UpdatedAt = time.Now().UTC()
	m.familyGroups[fg.ID] = existing
	return nil
}

func (m *Memory) DeleteFamilyGroup(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.familyGroups[id]; !ok {
		return ErrNotFound
	}
	delete(m.familyGroups, id)
	return nil
}

// ── Agent ──

func (m *Memory) UpsertAgent(_ context.Context, a models.Agent) error {
	if a.ID == "" {
		return errors.New("agent id required")
	}
	if a.FamilyGroupID == "" {
		return errors.New("family_group_id required")
	}
	if a.Status == "" {
		a.Status = "unknown"
	}
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.agents[a.ID]; ok {
		a.RegisteredAt = existing.RegisteredAt
		if a.DisplayName == "" {
			a.DisplayName = existing.DisplayName
		}
		if len(a.Labels) == 0 {
			a.Labels = existing.Labels
		}
	} else if a.RegisteredAt.IsZero() {
		a.RegisteredAt = now
	}
	a.UpdatedAt = now
	m.agents[a.ID] = a
	return nil
}

func (m *Memory) GetAgent(_ context.Context, id string) (models.Agent, bool, error) {
	m.mu.RLock()
	a, ok := m.agents[id]
	m.mu.RUnlock()
	return a, ok, nil
}

func (m *Memory) ListAgents(_ context.Context, familyGroupID string) ([]models.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		if familyGroupID != "" && a.FamilyGroupID != familyGroupID {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (m *Memory) ListAgentsByStatus(_ context.Context, status string) ([]models.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []models.Agent
	for _, a := range m.agents {
		if a.Status == status {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *Memory) UpdateAgentStatus(_ context.Context, id string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return ErrNotFound
	}
	a.Status = status
	a.UpdatedAt = time.Now().UTC()
	m.agents[id] = a
	return nil
}

func (m *Memory) UpdateAgentHeartbeat(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	a.LastHeartbeatAt = &now
	a.Status = "online"
	a.UpdatedAt = now
	m.agents[id] = a
	return nil
}

func (m *Memory) MarkStaleAgentsOffline(_ context.Context, timeout time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().UTC().Add(-timeout)
	count := 0
	for id, a := range m.agents {
		if a.Status == "online" && a.LastHeartbeatAt != nil && a.LastHeartbeatAt.Before(cutoff) {
			a.Status = "offline"
			a.UpdatedAt = time.Now().UTC()
			m.agents[id] = a
			count++
		}
	}
	return count, nil
}

func (m *Memory) UpdateAgentRiskScore(_ context.Context, id string, score float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return ErrNotFound
	}
	a.RiskScore = score
	a.UpdatedAt = time.Now().UTC()
	m.agents[id] = a
	return nil
}

// ── Audit events ──

func (m *Memory) AppendAuditEvents(_ context.Context, events []models.AuditEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range events {
		if e.EventID == "" || e.AgentID == "" {
			continue
		}
		if e.OccurredAt.IsZero() {
			e.OccurredAt = time.Now().UTC()
		}
		m.auditEvents = append(m.auditEvents, e)
		n++
		if len(m.auditEvents) > m.auditCap {
			overflow := len(m.auditEvents) - m.auditCap
			m.auditEvents = m.auditEvents[overflow:]
		}
	}
	return n, nil
}

func (m *Memory) ListAuditEvents(_ context.Context, limit int) ([]models.AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	start := len(m.auditEvents) - limit
	if start < 0 {
		start = 0
	}
	out := make([]models.AuditEvent, len(m.auditEvents)-start)
	copy(out, m.auditEvents[start:])
	// reverse for chronological order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (m *Memory) ListAuditEventsFiltered(_ context.Context, filter models.AuditEventFilter) ([]models.AuditEvent, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var filtered []models.AuditEvent
	for _, e := range m.auditEvents {
		if filter.AgentID != "" && e.AgentID != filter.AgentID {
			continue
		}
		if filter.FamilyGroupID != "" && e.FamilyGroupID != filter.FamilyGroupID {
			continue
		}
		if filter.Action != "" && e.Action != filter.Action {
			continue
		}
		if filter.FromTime != nil && e.OccurredAt.Before(*filter.FromTime) {
			continue
		}
		if filter.ToTime != nil && !e.OccurredAt.Before(*filter.ToTime) {
			continue
		}
		filtered = append(filtered, e)
	}
	total := len(filtered)
	// reverse chronological
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

// ── Policy bundles ──

func (m *Memory) CreatePolicyBundle(_ context.Context, pb models.PolicyBundle) error {
	pb.CreatedAt = time.Now().UTC()
	m.mu.Lock()
	m.policies = append(m.policies, pb)
	m.mu.Unlock()
	return nil
}

func (m *Memory) GetActivePolicyBundle(_ context.Context) (models.PolicyBundle, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := len(m.policies) - 1; i >= 0; i-- {
		if m.policies[i].Active {
			return m.policies[i], true, nil
		}
	}
	debugLog("GetActivePolicyBundle: no active bundle found", "store", "memory")
	return models.PolicyBundle{}, false, nil
}

func (m *Memory) SetPolicyBundleActive(_ context.Context, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.policies {
		m.policies[i].Active = false
	}
	for i := range m.policies {
		if m.policies[i].Version == version {
			m.policies[i].Active = true
			return nil
		}
	}
	return fmt.Errorf("policy version %q not found", version)
}

func (m *Memory) ListPolicyBundles(_ context.Context) ([]models.PolicyBundle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.PolicyBundle, len(m.policies))
	copy(out, m.policies)
	return out, nil
}

// ── Risk alerts ──

func (m *Memory) CreateRiskAlert(_ context.Context, alert models.RiskAlert) error {
	alert.CreatedAt = time.Now().UTC()
	m.mu.Lock()
	m.alerts = append(m.alerts, alert)
	m.mu.Unlock()
	return nil
}

func (m *Memory) ListRiskAlerts(_ context.Context, filter models.AlertFilter) ([]models.RiskAlert, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var filtered []models.RiskAlert
	for _, a := range m.alerts {
		if filter.FamilyGroupID != "" && a.FamilyGroupID != filter.FamilyGroupID {
			continue
		}
		if filter.Severity != "" && a.Severity != filter.Severity {
			continue
		}
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		filtered = append(filtered, a)
	}
	total := len(filtered)
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].OccurredAt.After(filtered[j].OccurredAt) })
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

func (m *Memory) UpdateRiskAlertStatus(_ context.Context, alertID string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.alerts {
		if a.AlertID == alertID {
			m.alerts[i].Status = status
			if status == "resolved" || status == "dismissed" {
				now := time.Now().UTC()
				m.alerts[i].ResolvedAt = &now
			}
			return nil
		}
	}
	return ErrNotFound
}

// ── Dashboard ──

func (m *Memory) GetDashboardStats(_ context.Context, familyGroupID string) (models.DashboardStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ds models.DashboardStats

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	for _, a := range m.agents {
		if familyGroupID != "" && a.FamilyGroupID != familyGroupID {
			continue
		}
		ds.AgentCount++
		if a.Status == "online" {
			ds.OnlineAgentCount++
		}
		if a.Status == "suspicious" {
			ds.SuspiciousAgentCount++
		}
	}

	eventCount := 0
	for _, e := range m.auditEvents {
		if familyGroupID != "" && e.FamilyGroupID != familyGroupID {
			continue
		}
		if e.OccurredAt.After(oneHourAgo) {
			eventCount++
		}
	}
	ds.EventRateLastHour = eventCount / 60

	for _, a := range m.alerts {
		if familyGroupID != "" && a.FamilyGroupID != familyGroupID {
			continue
		}
		if a.Status == "open" {
			ds.OpenAlertCount++
			if a.Severity == "critical" {
				ds.CriticalAlertCount++
			}
		}
	}

	// Recent alerts
	var openAlerts []models.RiskAlert
	for _, a := range m.alerts {
		if familyGroupID != "" && a.FamilyGroupID != familyGroupID {
			continue
		}
		if a.Status == "open" {
			openAlerts = append(openAlerts, a)
		}
	}
	sort.Slice(openAlerts, func(i, j int) bool { return openAlerts[i].OccurredAt.After(openAlerts[j].OccurredAt) })
	if len(openAlerts) > 5 {
		openAlerts = openAlerts[:5]
	}
	ds.RecentAlerts = openAlerts
	return ds, nil
}

// ── Token Quota ──

func (m *Memory) CreateTokenQuota(_ context.Context, q models.TokenQuota) error {
	if q.QuotaID == "" {
		return errors.New("quota_id required")
	}
	m.mu.Lock()
	m.tokenQuotas[q.QuotaID] = q
	m.mu.Unlock()
	return nil
}

func (m *Memory) GetTokenQuota(_ context.Context, targetType, targetID string) (models.TokenQuota, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, q := range m.tokenQuotas {
		if q.TargetType == targetType && q.TargetID == targetID && q.Active {
			return q, true, nil
		}
	}
	return models.TokenQuota{}, false, nil
}

func (m *Memory) ListTokenQuotas(_ context.Context, targetType string) ([]models.TokenQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []models.TokenQuota
	for _, q := range m.tokenQuotas {
		if targetType != "" && q.TargetType != targetType {
			continue
		}
		out = append(out, q)
	}
	return out, nil
}

func (m *Memory) UpdateTokenQuota(_ context.Context, q models.TokenQuota) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tokenQuotas[q.QuotaID]; !ok {
		return ErrNotFound
	}
	m.tokenQuotas[q.QuotaID] = q
	return nil
}

func (m *Memory) DeleteTokenQuota(_ context.Context, quotaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tokenQuotas[quotaID]; !ok {
		return ErrNotFound
	}
	delete(m.tokenQuotas, quotaID)
	return nil
}

// ── Token Usage Logs ──

func (m *Memory) AppendTokenUsageLog(_ context.Context, l models.TokenUsageLog) error {
	if l.LogID == "" {
		return errors.New("log_id required")
	}
	m.mu.Lock()
	m.tokenUsageLogs = append(m.tokenUsageLogs, l)
	m.mu.Unlock()
	return nil
}

func (m *Memory) GetTokenUsageLogs(_ context.Context, filter models.TokenUsageLogFilter) ([]models.TokenUsageLog, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var filtered []models.TokenUsageLog
	for _, l := range m.tokenUsageLogs {
		if filter.AgentID != "" && l.AgentID != filter.AgentID {
			continue
		}
		if filter.FamilyGroupID != "" && l.FamilyGroupID != filter.FamilyGroupID {
			continue
		}
		if filter.ModelName != "" && l.ModelName != filter.ModelName {
			continue
		}
		if filter.FromTime != "" && l.OccurredAt < filter.FromTime {
			continue
		}
		if filter.ToTime != "" && l.OccurredAt >= filter.ToTime {
			continue
		}
		filtered = append(filtered, l)
	}
	total := len(filtered)

	// reverse chronological
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

// ── Token Usage Summary ──

func (m *Memory) GetTokenUsageSummary(_ context.Context, targetType, targetID, period string) ([]models.TokenUsageSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []models.TokenUsageSummary
	for _, s := range m.usageSummaries {
		if s.TargetType == targetType && s.TargetID == targetID && s.Period == period {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *Memory) UpsertTokenUsageSummary(_ context.Context, summary models.TokenUsageSummary) error {
	summaryID := summary.TargetType + ":" + summary.TargetID + ":" + summary.Period + ":" + summary.DateKey
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.usageSummaries {
		sid := s.TargetType + ":" + s.TargetID + ":" + s.Period + ":" + s.DateKey
		if sid == summaryID {
			m.usageSummaries[i].InputTokens += summary.InputTokens
			m.usageSummaries[i].OutputTokens += summary.OutputTokens
			m.usageSummaries[i].TotalTokens += summary.TotalTokens
			m.usageSummaries[i].RequestCount += summary.RequestCount
			m.usageSummaries[i].CostMillicents += summary.CostMillicents
			return nil
		}
	}
	m.usageSummaries = append(m.usageSummaries, summary)
	return nil
}

// ── Model Prices ──

func (m *Memory) ListModelPrices(_ context.Context) ([]models.ModelPrice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.ModelPrice, 0, len(m.modelPrices))
	for _, p := range m.modelPrices {
		out = append(out, p)
	}
	return out, nil
}

func (m *Memory) UpsertModelPrice(_ context.Context, p models.ModelPrice) error {
	m.mu.Lock()
	m.modelPrices[p.ModelID] = p
	m.mu.Unlock()
	return nil
}
