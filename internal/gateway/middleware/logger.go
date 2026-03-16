package middleware

import (
	"Flux-KV/pkg/logger"
	"time"

	"github.com/gin-gonic/gin"
)

// AccessLog 异步日志中间件
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 开始计时
		start := time.Now()

		// 2. 处理请求
		c.Next()

		// 3. 请求结束
		latency := time.Since(start)

		// 获取可能的错误
		errMsg := ""
		if len(c.Errors) > 0 {
			errMsg = c.Errors.String()
		}

		// 4. 封装日志对象
		entry := &logger.AccessLogEntry{
			Method: c.Request.Method,
			Path: c.Request.URL.Path,
			Query: c.Request.URL.RawQuery,
			Status: c.Writer.Status(),
			ClientIP: c.ClientIP(),
			Latency: latency,
			UserAgent: c.Request.UserAgent(),
			Error: errMsg,
		}

		// 5. 扔进 Channel
		logger.WriteAccessLog(entry)
	}
}