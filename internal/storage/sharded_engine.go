package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"Flux-KV/internal/config"
	"Flux-KV/internal/event"
	"Flux-KV/internal/storage/aof"
	"Flux-KV/internal/storage/locker"
	"Flux-KV/internal/storage/shard"
)

// ShardedEngine 统一的存储引擎实现
// 通过组合 Locker（并发控制策略）和 []Shard（物理存储）实现不同特性
// 所有引擎实例（memdb、simplemap 等）都是 ShardedEngine 的不同配置
type ShardedEngine struct {
	shards     []shard.Shard
	locker     locker.Locker
	shardCount int
	hashFunc   func(string) uint32
	engineType string

	aof      *aof.AofHandler
	eventBus *event.EventBus
}

// NewShardedEngine 创建统一的存储引擎
// locker: 并发控制策略（全局锁/分片锁等）
// shardFactory: 创建单个分片物理存储的工厂函数
// engineType: 引擎类型标识（如 "memdb"、"simplemap"）
// cfg: 配置，用于初始化 AOF 和 EventBus
func NewShardedEngine(
	locker locker.Locker,
	shardFactory func() shard.Shard,
	engineType string,
	cfg *config.Config,
) (*ShardedEngine, error) {
	count := locker.ShardCount()
	shards := make([]shard.Shard, count)
	for i := 0; i < count; i++ {
		shards[i] = shardFactory()
	}

	e := &ShardedEngine{
		shards:     shards,
		locker:     locker,
		shardCount: count,
		hashFunc:   fnv32a,
		engineType: engineType,
	}

	// 初始化 RabbitMQ EventBus
	if cfg != nil && cfg.RabbitMQ.URL != "" {
		bus, err := event.NewEventBus(10000, cfg.RabbitMQ.URL, 4)
		if err != nil {
			log.Printf("[Warning] Failed to connect RabbitMQ: %v, EventBus disabled", err)
		} else {
			e.eventBus = bus
			e.eventBus.StartConsumer()
			log.Println("[EventBus] RabbitMQ connected success")
		}
	}

	// 初始化 AOF 模块
	if cfg != nil && cfg.AOF.Filename != "" {
		flushInterval := time.Duration(cfg.AOF.FlushIntervalMs) * time.Millisecond
		handler, err := aof.NewAofHandler(cfg.AOF.Filename, cfg.AOF.BatchSize, flushInterval)
		if err != nil {
			return nil, fmt.Errorf("failed to init AOF handler: %w", err)
		}
		e.aof = handler

		// 启动时恢复数据
		if err := e.loadFromAof(); err != nil {
			log.Printf("[Warning] Failed to load from AOF: %v", err)
		}
	}

	return e, nil
}

// Get 获取数据
// 使用单次 RLock，Shard.Get 已处理过期返回 false，过期清理由 Set/Delete 负责
func (e *ShardedEngine) Get(key string) (string, bool) {
	idx := e.hashFunc(key) % uint32(e.shardCount)

	unlock := e.locker.RLock(int(idx))
	val, found := e.shards[idx].Get(key)
	unlock()

	if found {
		return string(val), true
	}
	return "", false
}

// Set 写入数据
func (e *ShardedEngine) Set(key string, value string, ttl time.Duration) error {
	idx := e.hashFunc(key) % uint32(e.shardCount)

	// 1. 写锁下更新存储
	wunlock := e.locker.Lock(int(idx))
	e.shards[idx].Set(key, []byte(value), ttl)
	wunlock()

	// 2. AOF 异步写入（在锁外，避免阻塞并发）
	if e.aof != nil {
		cmd := aof.Cmd{
			Type:  "set",
			Key:   key,
			Value: value,
		}
		if err := e.aof.AsyncWrite(cmd); err != nil {
			log.Printf("[AOF] Write Error: %v", err)
		}
	}

	// 3. 投递事件到 EventBus（在锁外）
	if e.eventBus != nil {
		e.eventBus.Publish(event.Event{
			Type:  event.EventSet,
			Key:   key,
			Value: value,
		})
	}

	return nil
}

// Delete 删除数据
func (e *ShardedEngine) Delete(key string) error {
	idx := e.hashFunc(key) % uint32(e.shardCount)

	// 1. 写锁下删除
	wunlock := e.locker.Lock(int(idx))
	e.shards[idx].Delete(key)
	wunlock()

	// 2. AOF 异步写入
	if e.aof != nil {
		cmd := aof.Cmd{
			Type: "del",
			Key:  key,
		}
		if err := e.aof.AsyncWrite(cmd); err != nil {
			log.Printf("[AOF] Write Error: %v", err)
		}
	}

	// 3. 投递删除事件
	if e.eventBus != nil {
		e.eventBus.Publish(event.Event{
			Type: event.EventDel,
			Key:  key,
		})
	}

	return nil
}

// Stats 返回引擎统计信息
func (e *ShardedEngine) Stats() EngineStats {
	var totalEntries int64
	var totalMem int64
	for i := 0; i < e.shardCount; i++ {
		s := e.shards[i].Stats()
		if s.EntryCount >= 0 {
			totalEntries += s.EntryCount
		}
		totalMem += s.MemoryBytes
	}
	return EngineStats{
		EngineType:  e.engineType,
		EntryCount:  totalEntries,
		MemoryBytes: totalMem,
	}
}

// Close 优雅关闭引擎
func (e *ShardedEngine) Close() error {
	var errs []error

	// 1. 关闭 EventBus
	if e.eventBus != nil {
		if err := e.eventBus.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// 2. 关闭 AOF
	if e.aof != nil {
		if err := e.aof.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// 3. 关闭所有 shard
	for _, s := range e.shards {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// Scan 遍历所有未过期的 key-value
func (e *ShardedEngine) Scan(fn func(key string, val []byte)) {
	for i := 0; i < e.shardCount; i++ {
		unlock := e.locker.RLock(i)
		e.shards[i].Scan(func(key string, val []byte) {
			fn(key, val)
		})
		unlock()
	}
}

// Snapshot 序列化引擎当前状态
func (e *ShardedEngine) Snapshot() ([]byte, error) {
	data := make(map[string]string)
	e.Scan(func(key string, val []byte) {
		data[key] = string(val)
	})
	return json.Marshal(data)
}

// Restore 从快照恢复引擎状态
func (e *ShardedEngine) Restore(data []byte) error {
	var snapshot map[string]string
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	// 清空所有分片
	for i := 0; i < e.shardCount; i++ {
		unlock := e.locker.Lock(i)
		e.shards[i].Clear()
		unlock()
	}
	// 写入新数据（恢复时不设置 TTL）
	for k, v := range snapshot {
		idx := e.hashFunc(k) % uint32(e.shardCount)
		unlock := e.locker.Lock(int(idx))
		e.shards[idx].Set(k, []byte(v), 0)
		unlock()
	}
	return nil
}

// loadFromAof 从 AOF 文件恢复数据
func (e *ShardedEngine) loadFromAof() error {
	if e.aof == nil {
		return nil
	}

	cmds, err := e.aof.ReadAll()
	if err != nil {
		return fmt.Errorf("read AOF file error: %w", err)
	}

	for _, cmd := range cmds {
		idx := e.hashFunc(cmd.Key) % uint32(e.shardCount)
		wunlock := e.locker.Lock(int(idx))
		switch cmd.Type {
		case "set":
			e.shards[idx].Set(cmd.Key, []byte(cmd.Value), 0) // 恢复时不设置过期时间
		case "del":
			e.shards[idx].Delete(cmd.Key)
		}
		wunlock()
	}
	return nil
}

// fnv32a 实现 FNV-1a 32位哈希算法，用于 key 到 shard 的路由
func fnv32a(key string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	hash := uint32(offset32)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= prime32
	}
	return hash
}
