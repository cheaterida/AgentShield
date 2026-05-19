// Package main：分级处置工作器 — 订阅安全告警并执行四级差异化响应。
//
// 四级响应：
//   - low:    记录聚合，超阈值升级为 critical
//   - medium: 回滚至上一版本策略
//   - high:   柔性降级（限制为只读 + 人工审批）
//   - critical: 熔断（停止 Agent + 阻断 API 调用）
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentshield.dev/agentshield/error-handler/internal"
	"agentshield.dev/agentshield/error-handler/internal/workflow"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	mgmtURL := getenv("MANAGEMENT_SERVER_URL", "http://localhost:8080")

	executor := internal.NewExecutor(mgmtURL, logger)
	subscriber := internal.NewSubscriber(mgmtURL, logger)
	aggregator := workflow.NewAggregator(5, 300) // 5 low alerts in 5 min -> critical

	// 启动告警轮询
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				alerts, err := subscriber.PollOpenAlerts(ctx)
				if err != nil {
					logger.Error("poll alerts", "err", err)
					continue
				}
				for _, alert := range alerts {
					logger.Info("processing alert",
						"alert_id", alert.AlertID,
						"severity", alert.Severity,
						"agent_id", alert.AgentID,
					)

					switch alert.Severity {
					case "critical":
						if err := executor.CircuitBreak(ctx, &alert); err != nil {
							logger.Error("circuit break", "err", err)
						}
					case "high":
						if err := executor.Degrade(ctx, &alert); err != nil {
							logger.Error("degrade", "err", err)
						}
					case "medium":
						if err := executor.Rollback(ctx, &alert); err != nil {
							logger.Error("rollback", "err", err)
						}
					case "low":
						if agg := aggregator.Add(alert); agg != nil {
							logger.Warn("low alert threshold exceeded, escalating to critical",
								"agent_id", alert.AgentID,
							)
							if err := executor.CircuitBreak(ctx, agg); err != nil {
								logger.Error("escalated circuit break", "err", err)
							}
						}
					}
				}
			}
		}
	}()

	logger.Info("error-handler worker running")
	<-ctx.Done()
	logger.Info("error-handler worker stopped")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
