// Package risk provides rule-based and exponential-moving-average risk scoring
// with optional ML-powered hybrid scoring via the MLScorer interface.
package risk

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

type Engine struct {
	store       store.Store
	log         *slog.Logger
	mu          sync.RWMutex
	agentScores map[string]float64 // agentID -> EMA score
	alpha       float64             // EMA decay factor
	mlScorer    MLScorer            // optional ML pipeline
}

func NewEngine(st store.Store, log *slog.Logger) *Engine {
	return &Engine{
		store:       st,
		log:         log,
		agentScores: make(map[string]float64),
		alpha:       0.3,
	}
}

// SetMLScorer 注入 ML 评分器。如果为 nil，回退到纯规则评分。
func (e *Engine) SetMLScorer(scorer MLScorer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mlScorer = scorer
}

var defaultRules = []Rule{
	&SensitivePathRule{},
	&WriteActionRule{},
	&NetworkAccessRule{},
}

// Evaluate 对一批事件评分并更新 agent EMA。使用混合规则+ML评分。
func (e *Engine) Evaluate(ctx context.Context, events []models.AuditEvent) []models.RiskAlert {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Determine ML weight: ramps from 0.1 to 0.7 as training data grows
	mlWeight := e.computeMLWeight()

	// If ML is available, try batch scoring
	var mlScores []float64
	if e.mlScorer != nil && e.mlScorer.IsAvailable() && mlWeight > 0.05 {
		scores, err := e.mlScorer.ScoreEvents(ctx, events)
		if err != nil {
			e.log.Debug("ml scoring failed, using rules only", "err", err)
			mlWeight = 0
		} else {
			mlScores = scores
		}
	} else {
		mlWeight = 0
	}

	var alerts []models.RiskAlert
	for i := range events {
		ev := &events[i]

		// rule score (always computed)
		ruleScore := e.scoreEventRules(ev)

		// blend with ML score if available
		if mlWeight > 0 && i < len(mlScores) {
			ev.RiskContribution = mlWeight*mlScores[i] + (1-mlWeight)*ruleScore
		} else {
			if ev.RiskContribution == 0 {
				ev.RiskContribution = ruleScore
			}
		}

		prev := e.agentScores[ev.AgentID]
		ema := e.alpha*ev.RiskContribution + (1-e.alpha)*prev
		e.agentScores[ev.AgentID] = ema

		if alert := e.checkThresholds(ev, ema); alert != nil {
			alerts = append(alerts, *alert)
		}
	}

	// GNN/CFG scoring: group events by agent and call ScoreCFG for graph-level anomaly detection
	e.scoreCFGForAgents(ctx, events, mlWeight)

	return alerts
}

// scoreCFGForAgents groups events by agent and calls the GNN-based CFG scorer
// for agents with enough events to build a meaningful graph.
func (e *Engine) scoreCFGForAgents(ctx context.Context, events []models.AuditEvent, mlWeight float64) {
	if e.mlScorer == nil || !e.mlScorer.IsAvailable() || mlWeight <= 0.05 {
		return
	}

	// Group events by agent
	type agentData struct {
		actions      []string
		resourceRefs []string
	}
	byAgent := make(map[string]*agentData)
	for i := range events {
		ev := &events[i]
		ad, ok := byAgent[ev.AgentID]
		if !ok {
			ad = &agentData{}
			byAgent[ev.AgentID] = ad
		}
		ad.actions = append(ad.actions, ev.Action)
		ad.resourceRefs = append(ad.resourceRefs, ev.ResourceRef)
	}

	// Score CFG for each agent with ≥2 events
	for agentID, ad := range byAgent {
		if len(ad.actions) < 2 {
			continue
		}
		graphJSON := buildCFGJSON(agentID, ad.actions, ad.resourceRefs)
		cfgScore, err := e.mlScorer.ScoreCFG(ctx, agentID, graphJSON)
		if err != nil {
			e.log.Debug("cfg scoring failed", "agent_id", agentID, "err", err)
			continue
		}

		// Blend CFG score into agent's EMA (CFG weight is 0.3 of mlWeight)
		cfgWeight := mlWeight * 0.3
		currentEMA := e.agentScores[agentID]
		blendedScore := cfgWeight*cfgScore + (1-cfgWeight)*currentEMA
		e.agentScores[agentID] = blendedScore

		if cfgScore > 0.5 {
			e.log.Debug("cfg anomaly detected",
				"agent_id", agentID,
				"cfg_score", cfgScore,
				"ema_after", blendedScore,
			)
		}
	}
}

// computeMLWeight 根据训练数据量计算 ML 评分的权重。
// 权重从 0.1（冷启动）增长到 0.7（数据充足，2000+ 事件）。
func (e *Engine) computeMLWeight() float64 {
	if e.mlScorer == nil || !e.mlScorer.IsAvailable() {
		return 0
	}
	trainingEvents := e.mlScorer.TrainingEventCount()
	if trainingEvents < 500 {
		return 0 // not enough data
	}
	// Ramp linearly from 0.1 at 500 events to 0.7 at 2000
	weight := 0.1 + 0.6*float64(min(trainingEvents, 2000)-500)/1500.0
	if weight < 0 {
		weight = 0
	}
	if weight > 0.7 {
		weight = 0.7
	}
	return weight
}

func (e *Engine) scoreEventRules(ev *models.AuditEvent) float64 {
	var score float64
	for _, r := range defaultRules {
		s, _ := r.Evaluate(*ev)
		score += s
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}


func (e *Engine) checkThresholds(ev *models.AuditEvent, ema float64) *models.RiskAlert {
	var severity string
	if ema >= 0.8 {
		severity = "critical"
	} else if ema >= 0.6 {
		severity = "high"
	} else if ema >= 0.30 {
		severity = "medium"
	} else {
		return nil
	}

	e.log.Warn("risk alert triggered",
		"agent_id", ev.AgentID,
		"severity", severity,
		"score", ema,
		"resource", ev.ResourceRef,
	)

	return &models.RiskAlert{
		AlertID:       "alert_" + ev.EventID,
		FamilyGroupID: ev.FamilyGroupID,
		AgentID:       ev.AgentID,
		Severity:      severity,
		Title:         "Risk threshold exceeded for " + ev.AgentID,
		Description:   "EMA risk score " + formatScore(ema) + " on resource " + ev.ResourceRef,
		Status:        "open",
		OccurredAt:    ev.OccurredAt,
	}
}

func (e *Engine) GetAgentScore(agentID string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.agentScores[agentID]
}

func formatScore(s float64) string {
	return fmt.Sprintf("%.2f", s)
}
