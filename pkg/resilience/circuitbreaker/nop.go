package circuitbreaker

import "Flux-KV/pkg/resilience"

// Nop 是熔断器的空实现，始终允许所有请求通过
type Nop struct{}

// NewNop 创建空熔断器
func NewNop() resilience.CircuitBreaker {
	return &Nop{}
}

// Allow 始终返回 nil（允许通过）
func (n *Nop) Allow() error {
	return nil
}

// RecordSuccess 空操作
func (n *Nop) RecordSuccess() {}

// RecordFailure 空操作
func (n *Nop) RecordFailure() {}

// State 始终返回 Closed
func (n *Nop) State() resilience.CircuitState {
	return resilience.StateClosed
}
