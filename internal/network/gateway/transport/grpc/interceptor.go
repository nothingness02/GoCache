package grpc

import (
	"context"
	"fmt"
	"time"

	"Flux-KV/pkg/logger"
	"Flux-KV/pkg/metrics"
	"Flux-KV/pkg/resilience"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// 预分配常量，避免每次请求分配字符串
const (
	statusSuccess    = "success"
	statusError      = "error"
	labelGatewayGRPC = "gateway_grpc"
)

type requestIDKey struct{}

// RecoveryInterceptor 捕获 handler 中的 panic，防止进程崩溃
func RecoveryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Log.Error("[Gateway gRPC] panic recovered",
				zap.String("method", info.FullMethod),
				zap.Any("panic", r),
			)
			resp = nil
			err = status.Errorf(codes.Internal, "gateway internal panic: %v", r)
		}
	}()
	return handler(ctx, req)
}

// RequestIDInterceptor 从传入 metadata 中提取 request-id，若不存在则生成，
// 并将其注入到 outgoing context 以便透传到后端 Server
func RequestIDInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	var reqID string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-request-id"); len(vals) > 0 {
			reqID = vals[0]
		}
	}
	if reqID == "" {
		reqID = uuid.New().String()
	}
	ctx = context.WithValue(ctx, requestIDKey{}, reqID)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", reqID)
	return handler(ctx, req)
}

// RateLimitInterceptor 限流拦截器
func RateLimitInterceptor(limiter resilience.RateLimiter) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := limiter.Allow(ctx); err != nil {
			metrics.RateLimitedTotal.WithLabelValues(info.FullMethod).Inc()
			logger.Log.Warn("[Gateway gRPC] rate limited",
				zap.String("method", info.FullMethod),
				zap.String("reason", err.Error()),
			)
			return nil, status.Errorf(codes.ResourceExhausted, "rate limited: %v", err)
		}
		return handler(ctx, req)
	}
}

// CircuitBreakerInterceptor 熔断拦截器
func CircuitBreakerInterceptor(cb resilience.CircuitBreaker) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := cb.Allow(); err != nil {
			metrics.CircuitBreakerRejectedTotal.WithLabelValues(info.FullMethod).Inc()
			logger.Log.Warn("[Gateway gRPC] circuit breaker open",
				zap.String("method", info.FullMethod),
				zap.String("reason", err.Error()),
			)
			return nil, status.Errorf(codes.Unavailable, "circuit breaker: %v", err)
		}

		// 更新熔断器状态指标
		metrics.CircuitBreakerState.WithLabelValues(info.FullMethod).Set(float64(cb.State()))

		resp, err := handler(ctx, req)
		if err != nil {
			cb.RecordFailure()
		} else {
			cb.RecordSuccess()
		}
		// 请求完成后再次更新状态（状态可能已改变）
		metrics.CircuitBreakerState.WithLabelValues(info.FullMethod).Set(float64(cb.State()))
		return resp, err
	}
}

// MetricsInterceptor 统计 gRPC 方法调用次数和延迟
func MetricsInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	duration := time.Since(start).Seconds()

	method := info.FullMethod
	st := statusSuccess
	if err != nil {
		st = statusError
	}

	metrics.GRPCRequestsTotal.WithLabelValues(method, st).Inc()
	metrics.HTTPRequestDuration.WithLabelValues(labelGatewayGRPC, method, st).Observe(duration)

	return resp, err
}

// LoggingInterceptor 使用 Zap 记录每次 gRPC 调用的结构化日志
func LoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	duration := time.Since(start)

	fields := []zap.Field{
		zap.String("method", info.FullMethod),
		zap.Duration("duration", duration),
	}

	if reqID, ok := ctx.Value(requestIDKey{}).(string); ok && reqID != "" {
		fields = append(fields, zap.String("request_id", reqID))
	}

	if err != nil {
		st, _ := status.FromError(err)
		fields = append(fields,
			zap.String("status_code", st.Code().String()),
			zap.String("error", st.Message()),
		)
		logger.Log.Warn("[Gateway gRPC] request failed", fields...)
	} else {
		logger.Log.Info("[Gateway gRPC] request handled", fields...)
	}

	return resp, err
}

// DefaultUnaryInterceptors 返回 Gateway 默认的拦截器链（不含限流/熔断）
func DefaultUnaryInterceptors() []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		RecoveryInterceptor,
		RequestIDInterceptor,
		MetricsInterceptor,
		LoggingInterceptor,
	}
}

// formatError 辅助函数：格式化错误信息
func formatError(err error) string {
	if err == nil {
		return ""
	}
	st, ok := status.FromError(err)
	if ok {
		return fmt.Sprintf("%s: %s", st.Code().String(), st.Message())
	}
	return err.Error()
}
