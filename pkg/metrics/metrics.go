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
	// gRPC 请求数 (计数器)
	GRPCRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grpc_requests_total",
			Help: "Total number of gRPC requests",
		},
		[]string{"method", "type"},
	)
	// 当前连接数 (仪表)
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections",
			Help: "Number of active connections",
		},
	)
	// 业务指标 - SET 次数
	KVSetTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kv_set_total",
			Help: "Total number of SET operations",
		},
	)

	// 业务指标 - GET 次数
	KVGetTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "kv_get_total",
			Help: "Total number of GET operations",
		},
	)
)
