package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Executor 通过 REST API 向 management-server 和 agent-runtime 下发处置指令。
type Executor struct {
	mgmtURL string
	http    *http.Client
	log     *slog.Logger
}

func NewExecutor(mgmtURL string, log *slog.Logger) *Executor {
	return &Executor{
		mgmtURL: mgmtURL,
		http:    &http.Client{Timeout: 10 * time.Second},
		log:     log,
	}
}

// CircuitBreak 熔断：设置 agent 状态为 degraded 并确认告警。
func (e *Executor) CircuitBreak(ctx context.Context, alert *AlertInfo) error {
	e.log.Warn("CIRCUIT BREAK", "agent_id", alert.AgentID, "alert", alert.AlertID)

	// 标记告警为已确认
	e.ackAlert(ctx, alert.AlertID)

	// 将 agent 状态设为 degraded
	url := fmt.Sprintf("%s/api/v1/agents/%s/status", e.mgmtURL, alert.AgentID)
	body, _ := json.Marshal(map[string]string{"status": "degraded"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("circuit break: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// Degrade 降级：将 agent 设为 suspicious。
func (e *Executor) Degrade(ctx context.Context, alert *AlertInfo) error {
	e.log.Info("DEGRADE", "agent_id", alert.AgentID, "alert", alert.AlertID)
	e.ackAlert(ctx, alert.AlertID)

	url := fmt.Sprintf("%s/api/v1/agents/%s/status", e.mgmtURL, alert.AgentID)
	body, _ := json.Marshal(map[string]string{"status": "suspicious"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("degrade: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// Rollback 回滚：确认告警，agent 保持在线。
func (e *Executor) Rollback(ctx context.Context, alert *AlertInfo) error {
	e.log.Info("ROLLBACK", "agent_id", alert.AgentID, "alert", alert.AlertID)
	e.ackAlert(ctx, alert.AlertID)
	return nil
}

func (e *Executor) ackAlert(ctx context.Context, alertID string) {
	url := fmt.Sprintf("%s/api/v1/alerts/%s", e.mgmtURL, alertID)
	body, _ := json.Marshal(map[string]string{"status": "acknowledged"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		e.log.Error("ack alert", "alert_id", alertID, "err", err)
		return
	}
	resp.Body.Close()
}
