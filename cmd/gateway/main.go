package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Flux-KV/internal/config"
	"Flux-KV/internal/network/gateway/admin"
	gwgrpc "Flux-KV/internal/network/gateway/transport/grpc"
	"Flux-KV/pkg/logger"
	"Flux-KV/pkg/network/client"
	"Flux-KV/pkg/network/discovery"
	"Flux-KV/pkg/network/tracer"
	"Flux-KV/pkg/resilience"
	"Flux-KV/pkg/resilience/circuitbreaker"
	"Flux-KV/pkg/resilience/ratelimiter"

	pb "Flux-KV/api/proto"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	grpchealth "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

func main() {
	// 1. 初始化配置系统
	config.InitConfig()
	config.PrintConfig()

	// 2. 初始化日志
	logger.InitLogger()
	defer logger.Log.Sync()

	// 初始化分布式链路追踪
	jaegerEndpoint := viper.GetString("jaeger.endpoint")
	tp, err := tracer.InitTracer("gateway-service", jaegerEndpoint)
	if err != nil {
		logger.Log.Error("Failed to init tracer", zap.Error(err))
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			logger.Log.Error("Error shutting down tracer provider", zap.Error(err))
		}
	}()

	log := logger.Log
	log.Info("Gateway is starting...")

	// 3. 连接 Etcd 获取服务发现
	etcdEndpoints := viper.GetStringSlice("etcd.endpoints")
	log.Info("Connecting to Etcd...", zap.Strings("endpoints", etcdEndpoints))

	disco, err := discovery.NewDiscovery(etcdEndpoints)
	if err != nil {
		log.Fatal("Failed to connect to Etcd", zap.Error(err))
	}
	defer disco.Close()

	// 4. 初始化内部 gRPC Client（连接后端 Server）
	log.Info("Initializing internal KV Client (Resolver+Balancer)...")

	conn, err := client.NewGRPCConn(etcdEndpoints)
	if err != nil {
		log.Fatal("Failed to create gRPC connection", zap.Error(err))
	}
	defer func() {
		log.Info("Closing internal gRPC connection...")
		if err := conn.Close(); err != nil {
			log.Error("Failed to close gRPC connection", zap.Error(err))
		}
	}()
	kvClient := client.NewClient(conn)

	// 5. 启动 Gateway gRPC Server（对外）
	grpcPort := viper.GetInt("gateway.grpc_port")
	if grpcPort == 0 {
		grpcPort = 50051
	}

	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatal("Failed to listen gRPC", zap.Error(err))
	}

	// 初始化限流器
	var rateLimiter resilience.RateLimiter
	if viper.GetBool("gateway.rate_limiter.enabled") {
		rateLimiter = ratelimiter.NewTokenBucket(
			viper.GetFloat64("gateway.rate_limiter.rate"),
			viper.GetInt("gateway.rate_limiter.burst"),
		)
		log.Info("Rate limiter enabled",
			zap.Float64("rate", viper.GetFloat64("gateway.rate_limiter.rate")),
			zap.Int("burst", viper.GetInt("gateway.rate_limiter.burst")),
		)
	} else {
		rateLimiter = ratelimiter.NewNop()
		log.Info("Rate limiter disabled")
	}

	// 初始化熔断器
	var cb resilience.CircuitBreaker
	if viper.GetBool("gateway.circuit_breaker.enabled") {
		cb = circuitbreaker.NewSlidingWindow(circuitbreaker.SlidingWindowConfig{
			WindowSize:       viper.GetDuration("gateway.circuit_breaker.window_size"),
			FailureThreshold: viper.GetFloat64("gateway.circuit_breaker.failure_threshold"),
			MinCalls:         viper.GetInt("gateway.circuit_breaker.min_calls"),
			Cooldown:         viper.GetDuration("gateway.circuit_breaker.cooldown"),
			HalfOpenMaxCalls: viper.GetInt("gateway.circuit_breaker.half_open_max_calls"),
			SuccessThreshold: viper.GetInt("gateway.circuit_breaker.success_threshold"),
		})
		log.Info("Circuit breaker enabled",
			zap.Duration("window_size", viper.GetDuration("gateway.circuit_breaker.window_size")),
			zap.Float64("failure_threshold", viper.GetFloat64("gateway.circuit_breaker.failure_threshold")),
		)
	} else {
		cb = circuitbreaker.NewNop()
		log.Info("Circuit breaker disabled")
	}

	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Timeout:           10 * time.Second,
		}),
		grpc.ChainUnaryInterceptor(
			gwgrpc.RecoveryInterceptor,
			gwgrpc.RequestIDInterceptor,
			gwgrpc.RateLimitInterceptor(rateLimiter),
			gwgrpc.CircuitBreakerInterceptor(cb),
			gwgrpc.MetricsInterceptor,
			gwgrpc.LoggingInterceptor,
		),
	)

	// 注册 Gateway KV 服务
	pb.RegisterKVServiceServer(grpcServer, gwgrpc.NewGatewayKVServer(kvClient))

	// 注册 Health Check 服务
	healthSrv := health.NewServer()
	grpchealth.RegisterHealthServer(grpcServer, healthSrv)
	healthSrv.SetServingStatus("kv-service", grpchealth.HealthCheckResponse_SERVING)

	// 启用 gRPC 反射
	reflection.Register(grpcServer)

	go func() {
		log.Info("gRPC Server listening", zap.Int("port", grpcPort))
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Error("gRPC Server failed", zap.Error(err))
		}
	}()

	// 6. 启动 HTTP Server（/metrics + /admin）
	metricsPort := viper.GetInt("gateway.metrics_port")
	if metricsPort == 0 {
		metricsPort = 9090
	}
	adminHandler := admin.NewHandler(disco, rateLimiter, cb)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/admin/", adminHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ready := true
		var checks []string
		if disco != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := disco.Status(ctx); err != nil {
				ready = false
				checks = append(checks, "etcd: "+err.Error())
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "not_ready",
				"checks": checks,
				"time":   time.Now().Format(time.RFC3339),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ready",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	metricsSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", metricsPort),
		Handler: mux,
	}

	go func() {
		log.Info("Metrics/Admin HTTP server listening", zap.Int("port", metricsPort))
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Metrics HTTP server failed", zap.Error(err))
		}
	}()

	// 7. 条件启动 Pprof 监控服务
	if viper.GetBool("pprof.enabled") {
		pprofPort := viper.GetInt("pprof.port")
		go func() {
			log.Info("Pprof Debug Server is running",
				zap.String("addr", fmt.Sprintf("http://localhost:%d/debug/pprof/", pprofPort)))
			if err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", pprofPort), nil); err != nil {
				log.Error("Pprof Server failed", zap.Error(err))
			}
		}()
	}

	// 8. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutting down gateway...")

	// 停止健康检查
	healthSrv.SetServingStatus("kv-service", grpchealth.HealthCheckResponse_NOT_SERVING)

	// 关闭 gRPC Server
	grpcServer.GracefulStop()

	// 关闭 HTTP Server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(ctx); err != nil {
		log.Error("Metrics HTTP server shutdown error", zap.Error(err))
	}

	log.Info("Gateway exited properly")
}
