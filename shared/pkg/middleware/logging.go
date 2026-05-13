package middleware

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryLogging 对一元 gRPC 调用进行结构化日志记录。
func UnaryLogging(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		st, _ := status.FromError(err)
		log.Info("grpc unary",
			"method", info.FullMethod,
			"code", st.Code().String(),
			"dur_ms", time.Since(start).Milliseconds(),
		)
		return resp, err
	}
}

// StreamLogging 对流式 gRPC 调用进行结构化日志记录。
func StreamLogging(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		st, _ := status.FromError(err)
		log.Info("grpc stream",
			"method", info.FullMethod,
			"code", st.Code().String(),
			"dur_ms", time.Since(start).Milliseconds(),
		)
		return err
	}
}

// UnaryRecovery 捕获 panic 并返回 Internal 错误，防止服务端崩溃。
func UnaryRecovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("grpc panic recovered", "method", info.FullMethod, "panic", r)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}
