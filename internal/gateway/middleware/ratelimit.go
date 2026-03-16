package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// GlobalRateLimiter 定义全局限流中间件
// qps: 每秒产生的令牌数
// burst: 令牌桶最大容量
func GlobalRateLimiter(qps float64, burst int) gin.HandlerFunc {
	// 创建全局限流器
	limiter := rate.NewLimiter(rate.Limit(qps), burst)

	return func(c *gin.Context) {
		// 非阻塞尝试获取一个令牌
		if !limiter.Allow() {
			// 终止请求链，返回 429 JSON 相应
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "request limited",
				"msg": "服务器繁忙，请稍后再试",
				"ts": time.Now().Unix(),
			})
			return
		}

		c.Next()
	}
}