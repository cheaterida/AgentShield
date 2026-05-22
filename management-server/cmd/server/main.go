// Package main：AgentShield 监管中心 HTTP API。
//
// 六大模块落地：
//   1 智能体资产管理 — POST /api/v1/agents/register、GET /api/v1/agents 等
//   2 实时感知建模 — POST/GET /api/v1/audit/events，WebSocket 实时推送
//   3 风险评估决策 — risk.Engine 规则引擎 + EMA 评分
//   4 柔性防御执行 — 通过 heartbeat response 下发处置指令
//   5 权限资源管控 — policy.OPAClient 对接 OPA 策略评估
//   6 内核底座安全 — 见 kernel-hardening、ebpf-probes
//
// gRPC 已移除 (2026-05-20) — proto 定义保留在 shared/proto/ 作为契约参考。
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
	"agentshield.dev/agentshield/management-server/internal/cache"
	"agentshield.dev/agentshield/management-server/internal/config"
	"agentshield.dev/agentshield/management-server/internal/policy"
	"agentshield.dev/agentshield/management-server/internal/risk"
	"agentshield.dev/agentshield/management-server/internal/store"
	"agentshield.dev/agentshield/management-server/internal/token_quota"
)

func main() {
	cfg := config.Load()

	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Wire debug logger into store package for trace-level diagnostics.
	store.SetLogger(logger)

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

	// ── Redis 缓存层 (Track D1) ──
	var c cache.Cache
	if cfg.RedisAddr != "" {
		redisClient, err := cache.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
		if err != nil {
			logger.Warn("redis unavailable, cache disabled", "addr", cfg.RedisAddr, "err", err)
		} else {
			c = redisClient
			logger.Info("redis cache ready", "addr", cfg.RedisAddr, "db", cfg.RedisDB, "ttl", cfg.CacheTTL)
		}
	} else {
		logger.Info("cache disabled (AGENTSHIELD_REDIS_ADDR not set)")
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
	var regoContent string
	if cfg.OPAMode == "builtin" || cfg.OPAAllowBuiltinFallback {
		if cfg.OPARegoPath != "" {
			if data, err := os.ReadFile(cfg.OPARegoPath); err == nil {
				regoContent = string(data)
			} else {
				logger.Warn("failed to load rego file, builtin mode will be unavailable", "path", cfg.OPARegoPath, "err", err)
			}
		} else {
			logger.Warn("builtin mode or fallback enabled but AGENTSHIELD_OPA_REGO_PATH is not set")
		}
	}
	opaClient := policy.NewOPAClientWithConfig(policy.OPAConfig{
		BaseURL:              cfg.OPABaseURL,
		Mode:                 policy.OPAMode(cfg.OPAMode),
		RegoContent:          regoContent,
		AllowBuiltinFallback: cfg.OPAAllowBuiltinFallback,
	})
	logger.Info("opa client ready", "base_url", cfg.OPABaseURL, "mode", cfg.OPAMode, "fallback", cfg.OPAAllowBuiltinFallback)

	// ── Token 算力配额管理（可选）──
	var quotaMgr *token_quota.Manager
	if cfg.TokenQuotaEnabled {
		quotaMgr = token_quota.New(st, logger)
		logger.Info("token quota manager enabled",
			"default_daily", cfg.TokenQuotaDefaultDaily,
			"default_monthly", cfg.TokenQuotaDefaultMonthly)
	} else {
		logger.Info("token quota manager disabled (AGENTSHIELD_TOKEN_QUOTA_ENABLED=false)")
	}

	// ── HTTP API ──
	apiHandler := api.NewRouter(logger, st, riskEngine, wsHub, polDist, opaClient, quotaMgr)

	handler := apiHandler
	if c != nil {
		mux := http.NewServeMux()
		mux.Handle("/", apiHandler)
		if rc, ok := c.(*cache.RedisClient); ok {
			mux.Handle("GET /debug/metrics", rc.MetricsHandler())
		}
		handler = mux
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withLogging(logger, withCORS(handler)),
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

	// Agent offline detection — scan every 30s, mark agents with no heartbeat for 30s as offline.
	offlineCancel := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := st.MarkStaleAgentsOffline(context.Background(), 30*time.Second)
				if err != nil {
					logger.Warn("offline detection scan failed", "err", err)
				} else if n > 0 {
					logger.Info("marked stale agents as offline", "count", n)
				}
			case <-offlineCancel:
				return
			}
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
	close(offlineCancel)
	if c != nil {
		if rc, ok := c.(*cache.RedisClient); ok {
			if err := rc.Close(); err != nil {
				logger.Error("redis close", "err", err)
			}
		}
	}
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
