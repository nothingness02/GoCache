package picker

import (
	"sync"
	"time"
)

const (
	transitionTTL      = 10 * time.Minute
	decayInterval      = 30 * time.Second
	decayFactor        = 0.5
	hitThreshold       = 10.0
	maxBelowThresholds = 3
)

// RingMetrics 追踪旧环回退访问频率，支持基于衰减计数器的提前过期
type RingMetrics struct {
	mu              sync.Mutex
	oldRingHits     float64
	lastDecay       time.Time
	belowThresholds int
	prevRingAt      time.Time
}

// NewRingMetrics 创建 RingMetrics
func NewRingMetrics() *RingMetrics {
	now := time.Now()
	return &RingMetrics{
		lastDecay:  now,
		prevRingAt: now,
	}
}

// RecordHit 记录一次旧环回退访问
func (m *RingMetrics) RecordHit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oldRingHits++
}

// ShouldKeepPrevRing 判断是否应该继续保留旧环
// 基于 TTL 兜底 + 衰减计数器提前过期策略
func (m *RingMetrics) ShouldKeepPrevRing() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// TTL 兜底
	if time.Since(m.prevRingAt) > transitionTTL {
		return false
	}

	// 衰减计数器逻辑
	if time.Since(m.lastDecay) > decayInterval {
		m.oldRingHits *= decayFactor
		m.lastDecay = time.Now()
		if m.oldRingHits < hitThreshold {
			m.belowThresholds++
			if m.belowThresholds >= maxBelowThresholds {
				return false
			}
		} else {
			m.belowThresholds = 0
		}
	}

	return true
}

// OldRingHits 返回当前旧环回退命中计数（用于测试）
func (m *RingMetrics) OldRingHits() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.oldRingHits
}

// Reset 重置计数器（节点变更时调用）
func (m *RingMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oldRingHits = 0
	m.belowThresholds = 0
	now := time.Now()
	m.lastDecay = now
	m.prevRingAt = now
}
