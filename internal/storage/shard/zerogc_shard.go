package shard

import (
	"time"

	"Flux-KV/internal/storage/core"
)

// ZeroGCShardAdapter 将 core.ZeroGCShard 适配为 Shard 接口
type ZeroGCShardAdapter struct {
	data *core.ZeroGCShard
}

// NewZeroGCShard 创建基于 ZeroGCShard 的 Shard
func NewZeroGCShard(capacity uint32) *ZeroGCShardAdapter {
	return &ZeroGCShardAdapter{
		data: core.NewZeroGCShard(capacity),
	}
}

// Get 获取数据
func (s *ZeroGCShardAdapter) Get(key string) ([]byte, bool) {
	val, err := s.data.Get(key)
	if err != nil {
		return nil, false
	}
	return val, true
}

// Set 写入数据
func (s *ZeroGCShardAdapter) Set(key string, val []byte, ttl time.Duration) {
	s.data.Set(key, val, ttl)
}

// Delete 删除数据
func (s *ZeroGCShardAdapter) Delete(key string) {
	s.data.Delete(key)
}

// Stats 返回统计信息
// ZeroGCShard 不支持精确统计条目数，返回预分配容量作为内存估算
func (s *ZeroGCShardAdapter) Stats() ShardStats {
	return ShardStats{
		EntryCount:  -1, // 不支持精确统计
		MemoryBytes: int64(core.DefaultSharedSize),
	}
}

// Scan 遍历未过期的 key-value
func (s *ZeroGCShardAdapter) Scan(fn func(key string, val []byte)) {
	s.data.Scan(fn)
}

// Clear 清空分片
func (s *ZeroGCShardAdapter) Clear() {
	s.data.Clear()
}

// Close 关闭分片（无额外资源需要释放）
func (s *ZeroGCShardAdapter) Close() error {
	return nil
}
