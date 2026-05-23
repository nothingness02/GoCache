package locker

import "sync"

// GlobalRWMutex 使用单个全局读写锁保护所有分片
// 适用于低并发场景或需要严格串行语义的场景
type GlobalRWMutex struct {
	mu sync.RWMutex
}

// NewGlobalRWMutex 创建全局读写锁
func NewGlobalRWMutex() *GlobalRWMutex {
	return &GlobalRWMutex{}
}

func (l *GlobalRWMutex) Lock(_ int) func() {
	l.mu.Lock()
	return l.mu.Unlock
}

func (l *GlobalRWMutex) RLock(_ int) func() {
	l.mu.RLock()
	return l.mu.RUnlock
}

func (l *GlobalRWMutex) ShardCount() int {
	return 1
}

// ShardedRWMutex 为每个分片分配独立的读写锁
// 适用于高并发场景，多个 goroutine 可同时访问不同分片
type ShardedRWMutex struct {
	shards []sync.RWMutex
}

// NewShardedRWMutex 创建指定分片数量的分片锁
func NewShardedRWMutex(count int) *ShardedRWMutex {
	if count <= 0 {
		count = 1
	}
	return &ShardedRWMutex{
		shards: make([]sync.RWMutex, count),
	}
}

func (l *ShardedRWMutex) Lock(shardIdx int) func() {
	l.shards[shardIdx].Lock()
	return l.shards[shardIdx].Unlock
}

func (l *ShardedRWMutex) RLock(shardIdx int) func() {
	l.shards[shardIdx].RLock()
	return l.shards[shardIdx].RUnlock
}

func (l *ShardedRWMutex) ShardCount() int {
	return len(l.shards)
}
