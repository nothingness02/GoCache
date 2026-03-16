package middleware

import (
	"fmt"
	"net/http"

	"github.com/afex/hystrix-go/hystrix"
	"github.com/gin-gonic/gin"
)

// CircuitBreaker 熔断器中间件
// commandName 熔断器的名字，通常为服务名
func CircuitBreaker(commandName string) gin.HandlerFunc {
	// 1. 配置 Hystrix 命令
	hystrix.ConfigureCommand(commandName, hystrix.CommandConfig{
		Timeout:                16000, // 增加到 16000ms 适配 gRPC Client 15s 超时
		MaxConcurrentRequests:  100,   // 最大并发请求数
		RequestVolumeThreshold: 10,    // 触发熔断的最小请求量
		SleepWindow:            5000,  // 熔断冷却时间
		ErrorPercentThreshold:  50,    // 错误率阈值
	})

	return func(c *gin.Context) {
		// 2. 使用 hystrix.Do 包装后续的业务逻辑
		err := hystrix.Do(commandName, func() error {
			c.Next()

			// 根据相应状态码判断请求是否失败
			statusCode := c.Writer.Status()
			if statusCode >= http.StatusInternalServerError {
				return fmt.Errorf("downstream service error, status code: %d", statusCode)
			}
			return nil
		}, func(err error) error {
			// 3. 降级逻辑：熔断/超时/限流时执行
			if !c.Writer.Written() {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error":  "Service Unavailable (Circuit Breaker Triggered)",
					"detail": err.Error(),
				})
				c.Abort()
			}
			return nil
		})
		if err != nil {
			fmt.Printf("hystrix command %s error: %v\n", commandName, err)
		}
	}
}
