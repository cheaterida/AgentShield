// Package main：AgentShield 边缘网关 — Webhook 验证、租户路由、限流、审计中继。
//
// 架构：Envoy 作为数据面（TLS 终止、路径路由、限流），本进程处理
// 自定义鉴权与脱敏逻辑，通过 gRPC/HTTP 与管理端通信。
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

	"agentshield.dev/agentshield/gateway/internal"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	addr := getenv("LISTEN_ADDR", ":18080")
	mgmtURL := getenv("MANAGEMENT_SERVER_URL", "http://localhost:8080")

	auth := internal.NewWebhookAuth(getenv("WEBHOOK_SECRET", ""))
	limiter := internal.NewRateLimiter(100, 200) // 100 req/s burst 200
	relay := internal.NewRelay(mgmtURL, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Webhook 接收端点
	mux.HandleFunc("POST /webhook/{provider}", func(w http.ResponseWriter, r *http.Request) {
		provider := r.PathValue("provider")

		// HMAC 验证
		if err := auth.Verify(r); err != nil {
			logger.Warn("webhook auth failed", "provider", provider, "err", err)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// 限流（按 family_group_id header）
		fgid := r.Header.Get("X-Family-Group-ID")
		if !limiter.Allow(fgid) {
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}

		// 中继到 management-server
		if err := relay.Forward(r.Context(), r.Body); err != nil {
			logger.Error("relay forward", "err", err)
			http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"accepted":true}`))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           withLogging(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("gateway listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("gateway exit", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	<-ctx.Done()
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
	logger.Info("gateway stopped")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "dur_ms", time.Since(start).Milliseconds())
	})
}
