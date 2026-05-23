package storage

import "time"

// APStorage 包装 Engine，直接透传所有调用（AP 模式，高可用）
type APStorage struct {
	engine Engine
}

// NewAPStorage 创建 AP 模式存储
func NewAPStorage(engine Engine) *APStorage {
	return &APStorage{engine: engine}
}

// Get 直接读取本地引擎
func (ap *APStorage) Get(key string) (string, bool) {
	return ap.engine.Get(key)
}

// Set 直接写入本地引擎
func (ap *APStorage) Set(key string, value string, ttl time.Duration) error {
	return ap.engine.Set(key, value, ttl)
}

// Delete 直接删除本地引擎
func (ap *APStorage) Delete(key string) error {
	return ap.engine.Delete(key)
}

// Stats 返回引擎统计信息
func (ap *APStorage) Stats() EngineStats {
	stats := ap.engine.Stats()
	stats.EngineType = "ap-" + stats.EngineType
	return stats
}

// Scan 遍历所有未过期的 key-value（委托给底层引擎）
func (ap *APStorage) Scan(fn func(key string, val []byte)) {
	ap.engine.Scan(fn)
}

// Snapshot 序列化引擎当前状态（委托给底层引擎）
func (ap *APStorage) Snapshot() ([]byte, error) {
	return ap.engine.Snapshot()
}

// Restore 从快照恢复引擎状态（委托给底层引擎）
func (ap *APStorage) Restore(data []byte) error {
	return ap.engine.Restore(data)
}

// Close 关闭 AP 存储
func (ap *APStorage) Close() error {
	return ap.engine.Close()
}
