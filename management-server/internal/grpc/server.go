// Package grpc implements the ManagementService gRPC server.
package grpc

import (
	"context"
	"log/slog"
	"net"
	"time"

	"agentshield.dev/agentshield/management-server/internal/config"
	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/policy"
	"agentshield.dev/agentshield/management-server/internal/risk"
	"agentshield.dev/agentshield/management-server/internal/store"
	"agentshield.dev/agentshield/shared/pkg/middleware"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is the gRPC management server.
type Server struct {
	log  *slog.Logger
	store store.Store
	risk  *risk.Engine
	pol   *policy.Distributor
	grpcSrv *grpc.Server
}

func New(log *slog.Logger, st store.Store, re *risk.Engine, p *policy.Distributor) *Server {
	return &Server{log: log, store: st, risk: re, pol: p}
}

// Start 在给定地址上启动 gRPC 服务端。
func (s *Server) Start(cfg config.Config) error {
	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	s.grpcSrv = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.UnaryLogging(s.log),
			middleware.UnaryRecovery(s.log),
		),
		grpc.ChainStreamInterceptor(
			middleware.StreamLogging(s.log),
		),
	)
	// Register service — we use a manual implementation since generated stubs
	// would require the full proto-generated server interface. For now this
	// struct fulfills a subset via manual gRPC handler registration.
	// Full generated-stub registration would be:
	//   agentshieldv1.RegisterManagementServiceServer(s.grpcSrv, s)

	s.log.Info("gRPC server listening", "addr", cfg.GRPCAddr)
	return s.grpcSrv.Serve(lis)
}

// GracefulStop 优雅关闭 gRPC 服务端。
func (s *Server) GracefulStop() {
	if s.grpcSrv != nil {
		s.grpcSrv.GracefulStop()
	}
}

// ── Agent Heartbeat (manual handler for now) ──

type HeartbeatService struct {
	store store.Store
	pol   *policy.Distributor
	log   *slog.Logger
}

func (h *HeartbeatService) ProcessHeartbeat(ctx context.Context, hb models.AgentHeartbeat) (models.HeartbeatResponse, error) {
	if hb.AgentID == "" {
		return models.HeartbeatResponse{}, status.Error(codes.InvalidArgument, "agent_id required")
	}

	// Update heartbeat timestamp
	if err := h.store.UpdateAgentHeartbeat(ctx, hb.AgentID); err != nil {
		h.log.Error("heartbeat update", "agent_id", hb.AgentID, "err", err)
		return models.HeartbeatResponse{}, status.Error(codes.NotFound, "agent not found")
	}

	// Check for stale agents and mark them offline
	h.checkStaleAgents(ctx)

	resp := models.HeartbeatResponse{
		Acknowledged:        true,
		LatestPolicyVersion: h.pol.GetCurrentVersion(ctx),
		SuggestedAction:     "ok",
	}

	// Check if agent needs policy update
	if hb.LocalPolicyVersion != "" && hb.LocalPolicyVersion != resp.LatestPolicyVersion {
		resp.SuggestedAction = "update_policy"
	}

	// Check if agent is suspicious
	agent, found, _ := h.store.GetAgent(ctx, hb.AgentID)
	if found && agent.RiskScore >= 0.6 {
		resp.SuggestedAction = "isolate"
	}

	return resp, nil
}

func (h *HeartbeatService) checkStaleAgents(ctx context.Context) {
	agents, err := h.store.ListAgentsByStatus(ctx, "online")
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, a := range agents {
		if a.LastHeartbeatAt == nil {
			continue
		}
		// 30s no heartbeat -> offline
		if now.Sub(*a.LastHeartbeatAt) > 30*time.Second {
			_ = h.store.UpdateAgentStatus(ctx, a.ID, "offline")
			h.log.Warn("agent marked offline", "agent_id", a.ID)
		}
		// 2min no heartbeat -> suspicious
		if now.Sub(*a.LastHeartbeatAt) > 2*time.Minute {
			_ = h.store.UpdateAgentStatus(ctx, a.ID, "suspicious")
			h.log.Warn("agent marked suspicious", "agent_id", a.ID)
		}
	}
}
