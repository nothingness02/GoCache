package resilience

import "context"

// RateLimiter 限流器抽象接口
type RateLimiter interface {
	// Allow 检查单个请求是否被限流，返回 nil 表示允许通过
	Allow(ctx context.Context) error
	// AllowN 检查 n 个配额是否可用，返回 nil 表示允许通过
	AllowN(ctx context.Context, n int) error
}
