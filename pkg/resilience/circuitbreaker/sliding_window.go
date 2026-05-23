package circuitbreaker

import (
	"fmt"
	"sync"
	"time"

	"Flux-KV/pkg/resilience"
)

// SlidingWindowConfig 滑动窗口熔断器配置
type SlidingWindowConfig struct {
	WindowSize       time.Duration // 滑动窗口大小
	FailureThreshold float64       // 失败率阈值 (0.0 ~ 1.0)
	MinCalls         int           // 窗口内最少调用次数才触发判断
	Cooldown         time.Duration // Open -> HalfOpen 的冷却时间
	HalfOpenMaxCalls int           // 半开状态允许的最大探测请求数
	SuccessThreshold int           // 半开状态恢复所需连续成功次数
}

// DefaultSlidingWindowConfig 返回默认配置
func DefaultSlidingWindowConfig() SlidingWindowConfig {
	return SlidingWindowConfig{
		WindowSize:       10 * time.Second,
		FailureThreshold: 0.5,
		MinCalls:         5,
		Cooldown:         5 * time.Second,
		HalfOpenMaxCalls: 3,
		SuccessThreshold: 2,
	}
}

// slot 时间槽内统计
type slot struct {
	timestamp int64 // 秒级时间戳
	success   int
	failure   int
}

// SlidingWindow 基于滑动窗口的熔断器实现
type SlidingWindow struct {
	cfg   SlidingWindowConfig
	mu    sync.RWMutex
	state resilience.CircuitState

	// 时间窗口槽（秒级粒度）
	slots   []slot
	head    int // 当前槽索引
	slotNum int // 槽数量

	// Open 状态相关
	openAt time.Time // 进入 Open 状态的时间

	// HalfOpen 状态相关
	halfOpenCalls    int // 半开状态已接受的探测请求数
	halfOpenSuccess  int // 半开状态连续成功数
}

// NewSlidingWindow 创建滑动窗口熔断器
func NewSlidingWindow(cfg SlidingWindowConfig) resilience.CircuitBreaker {
	slotNum := int(cfg.WindowSize.Seconds())
	if slotNum < 1 {
		slotNum = 1
	}
	return &SlidingWindow{
		cfg:     cfg,
		state:   resilience.StateClosed,
		slots:   make([]slot, slotNum),
		slotNum: slotNum,
	}
}

// Allow 检查是否允许请求通过
func (s *SlidingWindow) Allow() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.state {
	case resilience.StateClosed:
		return nil // 正常通过

	case resilience.StateOpen:
		// 检查冷却时间是否已过
		if time.Since(s.openAt) >= s.cfg.Cooldown {
			s.state = resilience.StateHalfOpen
			s.halfOpenCalls = 0
			s.halfOpenSuccess = 0
			// 继续到 HalfOpen 逻辑
		} else {
			return fmt.Errorf("circuit breaker is OPEN (cooldown remaining: %v)", s.cfg.Cooldown-time.Since(s.openAt))
		}

	case resilience.StateHalfOpen:
		// 允许有限探测请求
	}

	// HalfOpen 逻辑
	if s.halfOpenCalls >= s.cfg.HalfOpenMaxCalls {
		return fmt.Errorf("circuit breaker is HALF-OPEN (max probe calls reached: %d)", s.cfg.HalfOpenMaxCalls)
	}
	s.halfOpenCalls++
	return nil
}

// RecordSuccess 记录成功
func (s *SlidingWindow) RecordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == resilience.StateHalfOpen {
		s.halfOpenSuccess++
		if s.halfOpenSuccess >= s.cfg.SuccessThreshold {
			s.transitionToClosed()
		}
		return
	}

	s.record(true)
}

// RecordFailure 记录失败
func (s *SlidingWindow) RecordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == resilience.StateHalfOpen {
		// 半开状态任一失败即重新熔断
		s.transitionToOpen()
		return
	}

	s.record(false)

	// Closed 状态下检查是否需要熔断
	total, failures := s.windowStats()
	if total >= s.cfg.MinCalls {
		failureRate := float64(failures) / float64(total)
		if failureRate >= s.cfg.FailureThreshold {
			s.transitionToOpen()
		}
	}
}

// State 返回当前状态
func (s *SlidingWindow) State() resilience.CircuitState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// record 向当前时间槽记录一次结果
func (s *SlidingWindow) record(success bool) {
	now := time.Now().Unix()
	idx := int(now) % s.slotNum

	if s.slots[idx].timestamp != now {
		// 时间槽过期，重置
		s.slots[idx] = slot{timestamp: now}
	}

	if success {
		s.slots[idx].success++
	} else {
		s.slots[idx].failure++
	}
}

// windowStats 统计当前窗口内的总调用次数和失败次数
func (s *SlidingWindow) windowStats() (total, failures int) {
	cutoff := time.Now().Unix() - int64(s.cfg.WindowSize.Seconds())
	for _, sl := range s.slots {
		if sl.timestamp > cutoff {
			total += sl.success + sl.failure
			failures += sl.failure
		}
	}
	return
}

// transitionToOpen 转换到 Open 状态
func (s *SlidingWindow) transitionToOpen() {
	s.state = resilience.StateOpen
	s.openAt = time.Now()
}

// transitionToClosed 转换到 Closed 状态
func (s *SlidingWindow) transitionToClosed() {
	s.state = resilience.StateClosed
	s.halfOpenCalls = 0
	s.halfOpenSuccess = 0
	// 清空窗口统计
	for i := range s.slots {
		s.slots[i] = slot{}
	}
}
