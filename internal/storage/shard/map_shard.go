package shard

import (
	"time"
)

// mapItem 封装值和过期时间
type mapItem struct {
	value    []byte
	expireAt time.Time
}

// MapShard 基于标准 map 的物理存储实现
// 调用方需通过 Locker 保证并发安全
type MapShard struct {
	data map[string]*mapItem
}

// NewMapShard 创建新的 MapShard
func NewMapShard() *MapShard {
	return &MapShard{
		data: make(map[string]*mapItem),
	}
}

// Get 获取数据。如果 key 已过期，返回 nil, false（不删除，删除由 ShardedEngine 在写锁下处理）
func (s *MapShard) Get(key string) ([]byte, bool) {
	item, ok := s.data[key]
	if !ok {
		return nil, false
	}
	// 检查是否过期：返回 false，但不删除（调用方可能持有读锁）
	if !item.expireAt.IsZero() && time.Now().After(item.expireAt) {
		return nil, false
	}
	return item.value, true
}

// Set 写入数据
func (s *MapShard) Set(key string, val []byte, ttl time.Duration) {
	var expireAt time.Time
	if ttl > 0 {
		expireAt = time.Now().Add(ttl)
	}
	s.data[key] = &mapItem{value: val, expireAt: expireAt}
}

// Delete 删除数据（幂等：删除不存在的 key 是安全的）
func (s *MapShard) Delete(key string) {
	delete(s.data, key)
}

// Stats 返回统计信息
func (s *MapShard) Stats() ShardStats {
	mem := int64(len(s.data)) * 64 // map 开销估算
	for k, v := range s.data {
		mem += int64(len(k) + len(v.value))
	}
	return ShardStats{
		EntryCount:  int64(len(s.data)),
		MemoryBytes: mem,
	}
}

// Scan 遍历未过期的 key-value
func (s *MapShard) Scan(fn func(key string, val []byte)) {
	for k, item := range s.data {
		if !item.expireAt.IsZero() && time.Now().After(item.expireAt) {
			continue
		}
		val := make([]byte, len(item.value))
		copy(val, item.value)
		fn(k, val)
	}
}

// Clear 清空分片
func (s *MapShard) Clear() {
	s.data = make(map[string]*mapItem)
}

// Close 关闭分片（无额外资源需要释放）
func (s *MapShard) Close() error {
	return nil
}
