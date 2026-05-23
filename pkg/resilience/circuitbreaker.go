package resilience

import "fmt"

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed CircuitState = iota // 正常通过
	StateHalfOpen                   // 半开，允许探测请求
	StateOpen                       // 熔断，拒绝请求
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// CircuitBreaker 熔断器抽象接口
type CircuitBreaker interface {
	// Allow 检查是否允许当前请求通过。
	// 返回 nil 表示允许；返回 error 表示拒绝（circuit open 或 half-open 探测名额已满）
	Allow() error
	// RecordSuccess 记录一次成功调用
	RecordSuccess()
	// RecordFailure 记录一次失败调用
	RecordFailure()
	// State 返回当前熔断器状态（用于监控）
	State() CircuitState
}
