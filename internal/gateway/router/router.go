package router

import (
	"Flux-KV/internal/gateway/handler"
	"Flux-KV/internal/gateway/middleware"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// NewRouter 初始化 Gin 引擎并注册所有路由
func NewRouter(kvHandler *handler.KVHandler, healthHandler *handler.HealthHandler) *gin.Engine {
	// 使用 New() 而不是 Default()，因为后者自带了同步的 Logger 和 Recovery
	r := gin.New()

	// 1. 注册中间件
	r.Use(gin.Recovery())
	r.Use(otelgin.Middleware("gateway-service"))
	// 异步访问日志中间件
	r.Use(middleware.AccessLog())
	// 全局限流中间件
	r.Use(middleware.GlobalRateLimiter(1000, 2000))

	// 2. 系统路由
	r.GET("/health", healthHandler.Ping) // 2. 业务路由
	v1 := r.Group("api/v1")

	// 熔断器中间件
	v1.Use(middleware.CircuitBreaker("kv-service"))
	{
		v1.POST("/kv", kvHandler.HandleSet)
		v1.GET("/kv", kvHandler.HandleGet)
		v1.DELETE("/kv", kvHandler.HandleDel)
	}

	return r
}
