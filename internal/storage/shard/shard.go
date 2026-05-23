// Package shard 定义存储引擎的物理存储单元接口
// 不同的底层数据结构（map、ZeroGCShard、跳表等）通过实现 Shard 接口注入到 ShardedEngine 中
package shard

import "time"

// Shard 单个分片的物理存储
// 调用方（ShardedEngine）通过 Locker 保证同一时刻只有一个 goroutine 访问同一个 shard
// Shard 实现内部不需要额外加锁
type Shard interface {
	// Get 获取 key 对应的值，不存在返回 nil, false
	Get(key string) ([]byte, bool)
	// Set 设置 key-value，ttl=0 表示永不过期
	Set(key string, val []byte, ttl time.Duration)
	// Delete 删除 key
	Delete(key string)
	// Stats 返回分片统计信息
	Stats() ShardStats
	// Scan 遍历分片中所有未过期的 key-value
	Scan(fn func(key string, val []byte))
	// Clear 清空分片所有数据
	Clear()
	// Close 关闭分片资源
	Close() error
}

// ShardStats 分片统计信息
type ShardStats struct {
	EntryCount  int64
	MemoryBytes int64
}
