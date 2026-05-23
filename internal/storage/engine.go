package storage

import "time"

// Engine 定义统一的存储引擎接口
type Engine interface {
	// Get 获取 key 对应的值，如果不存在返回 ("", false)
	Get(key string) (string, bool)
	// Set 设置 key-value，ttl=0 表示永不过期
	Set(key string, value string, ttl time.Duration) error
	// Delete 删除 key
	Delete(key string) error
	// Stats 返回引擎当前统计信息
	Stats() EngineStats
	// Scan 遍历所有未过期的 key-value（调用方只读，不修改数据）
	Scan(fn func(key string, val []byte))
	// Snapshot 序列化引擎当前状态
	Snapshot() ([]byte, error)
	// Restore 从快照恢复引擎状态
	Restore(data []byte) error
	// Close 优雅关闭引擎
	Close() error
}

// EngineStats 存储引擎运行时统计
type EngineStats struct {
	EngineType  string // "memdb" | "simplemap"
	EntryCount  int64
	MemoryBytes int64 // 估算内存占用
}
