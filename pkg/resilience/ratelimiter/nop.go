package ratelimiter

import (
	"context"

	"Flux-KV/pkg/resilience"
)

// Nop 是限流器的空实现，始终允许所有请求通过
type Nop struct{}

// NewNop 创建空限流器
func NewNop() resilience.RateLimiter {
	return &Nop{}
}

// Allow 始终返回 nil（允许通过）
func (n *Nop) Allow(ctx context.Context) error {
	return nil
}

// AllowN 始终返回 nil（允许通过）
func (n *Nop) AllowN(ctx context.Context, count int) error {
	return nil
}
