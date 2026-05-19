// Package main：AgentShield 监管中心 HTTP + gRPC API。
//
// 六大模块落地：
//   1 智能体资产管理 — POST /api/v1/agents/register、GET /api/v1/agents 等
//   2 实时感知建模 — POST/GET /api/v1/audit/events，WebSocket 实时推送
//   3 风险评估决策 — risk.Engine 规则引擎 + EMA 评分
//   4 柔性防御执行 — 通过 heartbeat response 下发处置指令
//   5 权限资源管控 — policy.OPAClient 对接 OPA 策略评估
//   6 内核底座安全 — 见 kernel-hardening、ebpf-probes
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentshield.dev/agentshield/management-server/internal/api"
	"agentshield.dev/agentshield/management-server/internal/config"
	"agentshield.dev/agentshield/management-server/internal/grpc"
	"agentshield.dev/agentshield/management-server/internal/policy"
	"agentshield.dev/agentshield/management-server/internal/risk"
	"agentshield.dev/agentshield/management-server/internal/store"
)

func main() {
	cfg := config.Load()

	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// ── 初始化存储 ──
	var st store.Store
	if cfg.DBDriver == "sqlite" {
		sqliteStore, err := store.NewSQLite(cfg.SQLitePath)
		if err != nil {
			logger.Error("sqlite init", "err", err)
			os.Exit(1)
		}
		defer sqliteStore.Close()
		st = sqliteStore
		logger.Info("sqlite store ready", "path", cfg.SQLitePath)
	} else {
		st = store.NewMemory(cfg.AuditBufferCap)
		logger.Info("memory store ready", "cap", cfg.AuditBufferCap)
	}

	// ── 初始化组件 ──
	riskEngine := risk.NewEngine(st, logger)

	// ML pipeline scorer (optional)
	if cfg.MLPipelineURL != "" {
		mlScorer := risk.NewHTTPMLScorer(cfg.MLPipelineURL, logger)
		riskEngine.SetMLScorer(mlScorer)
		logger.Info("ml scorer enabled", "url", cfg.MLPipelineURL)
	} else {
		logger.Info("ml scorer disabled (AGENTSHIELD_ML_PIPELINE_URL not set)")
	}

	wsHub := api.NewHub(logger)
	polDist := policy.NewDistributor(st, logger)

	// OPA 策略引擎客户端
	opaClient := policy.NewOPAClient(cfg.OPABaseURL)
	logger.Info("opa client ready", "base_url", cfg.OPABaseURL)

	// ── HTTP API ──
	apiHandler := api.NewRouter(logger, st, riskEngine, wsHub, polDist, opaClient)
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withLogging(logger, withCORS(apiHandler)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("management-server HTTP listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server exit", "err", err)
			os.Exit(1)
		}
	}()

	// ── gRPC Server ──
	grpcSrv := grpc.New(logger, st, riskEngine, polDist)
	go func() {
		if err := grpcSrv.Start(cfg); err != nil {
			logger.Error("grpc server exit", "err", err)
		}
	}()

	// ── 优雅关闭 ──
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	<-ctx.Done()
	stop()

	logger.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "err", err)
	}
	grpcSrv.GracefulStop()
	logger.Info("management-server stopped")
}

func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "dur_ms", time.Since(start).Milliseconds())
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
