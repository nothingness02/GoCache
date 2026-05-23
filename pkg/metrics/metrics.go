package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"method", "path", "status"},
	)

	GRPCRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_requests_total",
			Help: "Total number of gRPC requests",
		},
		[]string{"method", "type"},
	)

	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections",
			Help: "Number of active connections",
		},
	)

	KVSetTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kv_set_total",
			Help: "Total number of SET operations",
		},
	)

	KVGetTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kv_get_total",
			Help: "Total number of GET operations",
		},
	)

	// ===== 新增指标 =====

	// KVRequestsTotal 按操作和结果统计存储请求
	KVRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kv_requests_total",
			Help: "Total number of KV storage requests",
		},
		[]string{"op", "result"},
	)

	// RaftRole Raft 节点角色 (0=Follower, 1=Candidate, 2=Leader)
	RaftRole = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "raft_role",
			Help: "Current Raft role: 0=Follower, 1=Candidate, 2=Leader",
		},
		[]string{"node_id", "group_id"},
	)

	// RaftCommitIndex Raft 提交索引
	RaftCommitIndex = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "raft_commit_index",
			Help: "Current Raft commit index",
		},
		[]string{"node_id", "group_id"},
	)

	// RaftHealthyNodes Raft 组健康节点数
	RaftHealthyNodes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "raft_healthy_nodes",
			Help: "Number of healthy nodes in Raft group",
		},
		[]string{"group_id"},
	)

	// EngineMemoryBytes 引擎内存占用
	EngineMemoryBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "engine_memory_bytes",
			Help: "Estimated memory bytes used by engine",
		},
		[]string{"engine_type", "node_id"},
	)

	// GatewayRequestsTotal 网关请求按一致性模式统计
	GatewayRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total number of gateway requests by consistency mode",
		},
		[]string{"consistency", "status"},
	)

	// GatewayBackendNodes 后端节点数量
	GatewayBackendNodes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gateway_backend_nodes",
			Help: "Number of backend nodes by mode",
		},
		[]string{"mode"},
	)

	// RateLimitedTotal 被限流的请求总数
	RateLimitedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rate_limited_total",
			Help: "Total number of rate limited requests",
		},
		[]string{"method"},
	)

	// CircuitBreakerState 熔断器当前状态 (0=closed, 1=half-open, 2=open)
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "Current circuit breaker state: 0=closed, 1=half-open, 2=open",
		},
		[]string{"method"},
	)

	// CircuitBreakerRejectedTotal 被熔断器拒绝的请求总数
	CircuitBreakerRejectedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_rejected_total",
			Help: "Total number of circuit breaker rejected requests",
		},
		[]string{"method"},
	)
)
