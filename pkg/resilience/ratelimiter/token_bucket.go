package ratelimiter

import (
	"context"
	"fmt"
	"time"

	"Flux-KV/pkg/resilience"
	"golang.org/x/time/rate"
)

// TokenBucket 基于 golang.org/x/time/rate 的令牌桶限流器实现
type TokenBucket struct {
	limiter *rate.Limiter
	rate    float64
	burst   int
}

// NewTokenBucket 创建令牌桶限流器
//   - r: 每秒产生的令牌数 (rate per second)
//   - burst: 桶的容量（允许的最大突发请求数）
func NewTokenBucket(r float64, burst int) resilience.RateLimiter {
	return &TokenBucket{
		limiter: rate.NewLimiter(rate.Limit(r), burst),
		rate:    r,
		burst:   burst,
	}
}

// Allow 检查单个请求是否被限流
func (t *TokenBucket) Allow(ctx context.Context) error {
	if !t.limiter.Allow() {
		return fmt.Errorf("rate limited: rate=%.2f/s burst=%d", t.rate, t.burst)
	}
	return nil
}

// AllowN 检查 n 个配额是否可用
func (t *TokenBucket) AllowN(ctx context.Context, n int) error {
	if !t.limiter.AllowN(time.Now(), n) {
		return fmt.Errorf("rate limited: requested=%d rate=%.2f/s burst=%d", n, t.rate, t.burst)
	}
	return nil
}
