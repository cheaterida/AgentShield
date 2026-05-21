package token_quota

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

type Manager struct {
	store store.Store
	log   *slog.Logger
}

func New(st store.Store, log *slog.Logger) *Manager {
	return &Manager{store: st, log: log}
}

// RecordUsage 处理 span 中的 token 用量：提取 → 算费 → 写日志 → 更新聚合 → 检查配额。
// 返回配额超标时生成的告警（nil 表示未超标）。
func (m *Manager) RecordUsage(ctx context.Context, agentID, familyGroupID string, attrs map[string]string) *models.RiskAlert {
	if len(attrs) == 0 {
		return nil
	}
	model, input, output, total := extractTokenUsage(attrs)
	if model == "" && total == 0 {
		return nil // 没有 token 数据
	}

	now := time.Now().UTC()
	occurredAt := now.Format(time.RFC3339Nano)

	// 查定价
	prices, err := m.store.ListModelPrices(ctx)
	if err != nil {
		m.log.Error("token_quota: list model prices", "err", err)
		return nil
	}
	cost := calculateCost(model, input, output, prices)

	// 写 usage log
	logID := fmt.Sprintf("tu_%s_%d", agentID, now.UnixNano())
	spanID := attrs["span_id"]
	traceID := attrs["trace_id"]
	provider := lookupProvider(model, prices)

	usageLog := models.TokenUsageLog{
		LogID:          logID,
		AgentID:        agentID,
		FamilyGroupID:  familyGroupID,
		SpanID:         spanID,
		TraceID:        traceID,
		ModelName:      model,
		Provider:       provider,
		InputTokens:    input,
		OutputTokens:   output,
		TotalTokens:    total,
		CostMillicents: cost,
		QuotaStatus:    "ok",
		OccurredAt:     occurredAt,
	}

	// 检查配额
	targetType := "agent"
	targetID := agentID
	quota, found, err := m.store.GetTokenQuota(ctx, targetType, targetID)
	if err != nil {
		m.log.Error("token_quota: get quota", "target_type", targetType, "target_id", targetID, "err", err)
	} else if !found {
		// fallback 到 family_group 配额
		targetType = "family_group"
		targetID = familyGroupID
		quota, found, err = m.store.GetTokenQuota(ctx, targetType, targetID)
		if err != nil {
			m.log.Error("token_quota: get family_group quota", "target_type", targetType, "target_id", targetID, "err", err)
		}
	}

	var projected int64
	var ratio float64
	if found && quota.DailyLimit > 0 {
		dateKey := now.Format("2006-01-02")
		summaries, _ := m.store.GetTokenUsageSummary(ctx, targetType, targetID, "daily")
		var dailyUsed int64
		for _, s := range summaries {
			if s.DateKey == dateKey {
				dailyUsed = s.TotalTokens
				break
			}
		}
		projected = dailyUsed + total
		ratio = float64(projected) / float64(quota.DailyLimit)

		if ratio >= quota.BlockThreshold {
			usageLog.QuotaStatus = "blocked"
		} else if ratio >= quota.WarnThreshold {
			usageLog.QuotaStatus = "warned"
		}
	}

	// 持久化日志
	if err := m.store.AppendTokenUsageLog(ctx, usageLog); err != nil {
		m.log.Error("token_quota: append usage log", "err", err)
		return nil
	}

	// 更新聚合（daily / weekly / monthly / total）
	m.upsertSummary(ctx, "agent", agentID, now, input, output, total, cost)
	if familyGroupID != "" {
		m.upsertSummary(ctx, "family_group", familyGroupID, now, input, output, total, cost)
	}

	// 超标时生成告警
	if usageLog.QuotaStatus == "blocked" && found && quota.DailyLimit > 0 {
		alert := &models.RiskAlert{
			AlertID:       "tq_blocked_" + logID,
			FamilyGroupID: familyGroupID,
			AgentID:       agentID,
			Severity:      "medium",
			Title:         "Token 配额已超限",
			Description:   fmt.Sprintf("Agent %s 的 Token 消耗已超过日限额 %d，当前用量比例 %.0f%%", agentID, quota.DailyLimit, ratio*100),
			Status:        "open",
			OccurredAt:    now,
		}
		return alert
	}
	if usageLog.QuotaStatus == "warned" && found && quota.DailyLimit > 0 {
		alert := &models.RiskAlert{
			AlertID:       "tq_warned_" + logID,
			FamilyGroupID: familyGroupID,
			AgentID:       agentID,
			Severity:      "info",
			Title:         "Token 配额接近上限",
			Description:   fmt.Sprintf("Agent %s 的 Token 消耗已达到日限额的 %.0f%%", agentID, ratio*100),
			Status:        "open",
			OccurredAt:    now,
		}
		return alert
	}

	return nil
}

// CheckQuota 检查 Agent 当前配额状态。返回 (status, todayUsed, dailyLimit)。
// status: ok | warned | blocked | no_quota
func (m *Manager) CheckQuota(ctx context.Context, agentID string) (string, int64, int64) {
	targetType := "agent"
	targetID := agentID
	quota, found, err := m.store.GetTokenQuota(ctx, targetType, targetID)
	if err != nil {
		m.log.Error("token_quota: check quota", "agent_id", agentID, "err", err)
		return "no_quota", 0, 0
	}
	if !found {
		return "no_quota", 0, 0
	}
	if quota.DailyLimit <= 0 {
		return "ok", 0, 0
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	summaries, _ := m.store.GetTokenUsageSummary(ctx, targetType, targetID, "daily")
	var dailyUsed int64
	for _, s := range summaries {
		if s.DateKey == dateKey {
			dailyUsed = s.TotalTokens
			break
		}
	}

	ratio := float64(dailyUsed) / float64(quota.DailyLimit)
	if ratio >= quota.BlockThreshold {
		return "blocked", dailyUsed, quota.DailyLimit
	}
	if ratio >= quota.WarnThreshold {
		return "warned", dailyUsed, quota.DailyLimit
	}
	return "ok", dailyUsed, quota.DailyLimit
}

// GetAgentUsage 返回 Agent 的四维用量汇总。
func (m *Manager) GetAgentUsage(ctx context.Context, agentID string) (map[string]int64, error) {
	usage := map[string]int64{
		"daily_input_tokens":   0,
		"daily_output_tokens":  0,
		"daily_total_tokens":   0,
		"daily_cost_millicents": 0,
		"monthly_input_tokens":   0,
		"monthly_output_tokens":  0,
		"monthly_total_tokens":   0,
		"monthly_cost_millicents": 0,
		"total_input_tokens":     0,
		"total_output_tokens":    0,
		"total_total_tokens":     0,
		"total_cost_millicents":  0,
	}

	now := time.Now().UTC()
	dateKey := now.Format("2006-01-02")
	monthKey := now.Format("2006-01")

	if s, err := m.store.GetTokenUsageSummary(ctx, "agent", agentID, "daily"); err == nil {
		for _, v := range s {
			if v.DateKey == dateKey {
				usage["daily_input_tokens"] = v.InputTokens
				usage["daily_output_tokens"] = v.OutputTokens
				usage["daily_total_tokens"] = v.TotalTokens
				usage["daily_cost_millicents"] = v.CostMillicents
			}
		}
	}

	if s, err := m.store.GetTokenUsageSummary(ctx, "agent", agentID, "monthly"); err == nil {
		for _, v := range s {
			if v.DateKey == monthKey {
				usage["monthly_input_tokens"] = v.InputTokens
				usage["monthly_output_tokens"] = v.OutputTokens
				usage["monthly_total_tokens"] = v.TotalTokens
				usage["monthly_cost_millicents"] = v.CostMillicents
			}
		}
	}

	if s, err := m.store.GetTokenUsageSummary(ctx, "agent", agentID, "total"); err == nil {
		for _, v := range s {
			if v.DateKey == "total" {
				usage["total_input_tokens"] = v.InputTokens
				usage["total_output_tokens"] = v.OutputTokens
				usage["total_total_tokens"] = v.TotalTokens
				usage["total_cost_millicents"] = v.CostMillicents
			}
		}
	}

	return usage, nil
}

// ── helpers ──

func (m *Manager) upsertSummary(ctx context.Context, targetType, targetID string, t time.Time, input, output, total, cost int64) {
	dateKey := t.Format("2006-01-02")
	_, week := t.ISOWeek()
	weekKey := fmt.Sprintf("%d-W%02d", t.Year(), week)
	monthKey := t.Format("2006-01")

	summaries := []models.TokenUsageSummary{
		{TargetType: targetType, TargetID: targetID, Period: "daily", DateKey: dateKey, InputTokens: input, OutputTokens: output, TotalTokens: total, RequestCount: 1, CostMillicents: cost},
		{TargetType: targetType, TargetID: targetID, Period: "weekly", DateKey: weekKey, InputTokens: input, OutputTokens: output, TotalTokens: total, RequestCount: 1, CostMillicents: cost},
		{TargetType: targetType, TargetID: targetID, Period: "monthly", DateKey: monthKey, InputTokens: input, OutputTokens: output, TotalTokens: total, RequestCount: 1, CostMillicents: cost},
		{TargetType: targetType, TargetID: targetID, Period: "total", DateKey: "total", InputTokens: input, OutputTokens: output, TotalTokens: total, RequestCount: 1, CostMillicents: cost},
	}
	for _, s := range summaries {
		if err := m.store.UpsertTokenUsageSummary(ctx, s); err != nil {
			m.log.Error("token_quota: upsert summary", "period", s.Period, "err", err)
		}
	}
}

// extractTokenUsage 从 span attributes 提取 token 用量字段。
func extractTokenUsage(attrs map[string]string) (model string, input, output, total int64) {
	model = attrs["gen_ai.response.model"]
	if model == "" {
		model = attrs["gen_ai.request.model"]
	}
	if v, ok := attrs["gen_ai.usage.input_tokens"]; ok {
		input, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := attrs["gen_ai.usage.output_tokens"]; ok {
		output, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := attrs["gen_ai.usage.total_tokens"]; ok {
		total, _ = strconv.ParseInt(v, 10, 64)
	}
	if total == 0 {
		total = input + output
	}
	return
}

// calculateCost 根据模型定价计算费用，返回毫分（万分之美元）。
func calculateCost(model string, input, output int64, prices []models.ModelPrice) int64 {
	var inputPrice, outputPrice int64
	for _, p := range prices {
		if p.ModelID == model && p.Active {
			inputPrice = p.InputPriceMillicents
			outputPrice = p.OutputPriceMillicents
			break
		}
	}
	// 价格单位是 毫分/1M tokens
	cost := (input * inputPrice / 1000000) + (output * outputPrice / 1000000)
	return cost
}

func lookupProvider(model string, prices []models.ModelPrice) string {
	for _, p := range prices {
		if p.ModelID == model {
			return p.Provider
		}
	}
	// 从模型 ID 推断 provider
	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") {
		return "openai"
	}
	if strings.HasPrefix(model, "claude-") {
		return "anthropic"
	}
	if strings.HasPrefix(model, "deepseek-") {
		return "deepseek"
	}
	if strings.HasPrefix(model, "qwen-") {
		return "alibaba"
	}
	return ""
}
