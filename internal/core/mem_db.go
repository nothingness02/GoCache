package core

import (
	"Flux-KV/internal/aof"
	"Flux-KV/internal/config"
	"Flux-KV/internal/event"
	"fmt"
	"log"
	"sync"
	"time"
)

// 定义分片数量，在大并发下足够减少锁冲突
const ShardCount = 256
const DefaultSharedSize = 10 * 1024 * 1024 // 每个分片默认64MB
// Item 封装了值和过期时间
type Item struct {
	Val      []byte
	ExpireAt int64
}

// 定义分片结构
type shard struct {
	mu   sync.RWMutex
	data *ZeroGCShard
}

// MemDB 内存数据库核心结构
type MemDB struct {
	shards     []*shard
	aofHandler *aof.AofHandler // 持有AOF操作对象
	eventBus   *event.EventBus // 持有 EventBus 指针
}

// FNV-1a hash constants
const (
	offset32 = 2166136261
	prime32  = 16777619
)

// 实现 FNV-1a 哈希算法
// 公式：hash = (hash ^ byte) * prime
func fnv32(key string) uint32 {
	hash := uint32(offset32)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= prime32
	}
	return hash
}

// getShard 根据 Key 路由到指定的分片
func (db *MemDB) getShard(key string) *shard {
	hash := fnv32(key)
	return db.shards[hash%ShardCount]
}

func NewMemDB(cfg *config.Config) (*MemDB, error) {
	db := &MemDB{
		shards: make([]*shard, ShardCount),
	}

	// 初始化所有分片
	for i := 0; i < ShardCount; i++ {
		db.shards[i] = &shard{
			data: NewZeroGCShard(DefaultSharedSize),
		}
	}

	// 初始化 RabbitMQ EventBus
	// 缓冲区设为 10000，足够应对瞬间的并发洪峰
	// 消费者数量设为 4，可根据机器配置调整
	if cfg.RabbitMQ.URL != "" {
		bus, err := event.NewEventBus(10000, cfg.RabbitMQ.URL, 4)
		if err != nil {
			// 如果 MQ 连不上，记录错误但允许系统继续运行（降级）
			log.Printf("⚠️ [Warning] Failed to connect RabbitMQ: %v, EventBus disabled.", err)
		} else {
			db.eventBus = bus
			db.eventBus.StartConsumer()
			log.Println("🔌 RabbitMQ connected success!")
		}
	}

	// 初始化 AOF 模块
	if cfg.AOF.Filename != "" {
		handler, err := aof.NewAofHandler(cfg.AOF.Filename)
		if err != nil {
			return nil, fmt.Errorf("failed to init AOF handler: %w", err)
		}
		db.aofHandler = handler

		// 启动时立刻恢复数据
		if err := db.loadFromAof(); err != nil {
			log.Printf("⚠️ [Warning] Failed to load from AOF: %v", err)
		}
	}

	return db, nil
}

// loadFromAof 从 AOF 文件恢复数据
func (db *MemDB) loadFromAof() error {
	if db.aofHandler == nil {
		return nil
	}

	// 读取所有命令
	cmds, err := db.aofHandler.ReadAll()
	if err != nil {
		return fmt.Errorf("read AOF file error: %w", err)
	}

	// 重放命令，针对每个 Key 找分片锁
	for _, cmd := range cmds {
		s := db.getShard(cmd.Key)
		s.mu.Lock()
		switch cmd.Type {
		case "set":
			s.data.Set(cmd.Key, []byte(cmd.Value), 0) // 恢复时不设置过期时间，等同于永久存储
		case "del":
			s.data.Delete(cmd.Key)
		}
		s.mu.Unlock()
	}
	return nil
}

// Set 写入数据，支持过期时间(ttl: time to live)
// ttl = 0 表示永不过期
func (db *MemDB) Set(key string, val any, ttl time.Duration) {
	// 1. 定位分片
	s := db.getShard(key)

	// 2. 分片加锁（细粒度）
	s.mu.Lock()
	s.data.Set(key, val.([]byte), ttl)
	s.mu.Unlock()

	// 3. 写 AOF
	if db.aofHandler != nil {
		cmd := aof.Cmd{
			Type:  "set",
			Key:   key,
			Value: val.(string), // 这里假设 val 是 string 类型，实际使用中可能需要更灵活的序列化
		}
		if err := db.aofHandler.AsyncWrite(cmd); err != nil {
			log.Printf("❌ AOF Write Error: %v", err)
		}
	}

	// 4. 投递事件到 EventBus
	if db.eventBus != nil {
		db.eventBus.Publish(event.Event{
			Type:  event.EventSet,
			Key:   key,
			Value: val,
		})
	}
}

// Get 获取数据（实现惰性删除）
func (db *MemDB) Get(key string) (any, bool) {
	s := db.getShard(key)

	// 1. 分片读锁
	s.mu.RLock()
	item, err := s.data.Get(key)
	s.mu.RUnlock()

	if err != nil {
		return nil, false
	}
	return item, true
}

// Del 手动删除数据
func (db *MemDB) Del(key string) {
	s := db.getShard(key)

	s.mu.Lock()
	// 删内存
	s.data.Delete(key)
	s.mu.Unlock()

	// 写 AOF
	if db.aofHandler != nil {
		cmd := aof.Cmd{
			Type: "del",
			Key:  key,
		}
		if err := db.aofHandler.AsyncWrite(cmd); err != nil {
			log.Printf("❌ AOF Write Error: %v", err)
		}
	}

	// 投递删除事件
	if db.eventBus != nil {
		db.eventBus.Publish(event.Event{
			Type: event.EventDel,
			Key:  key,
		})
	}
}

// 优雅关闭数据库
func (db *MemDB) Close() error {
	var errs []error

	// 1. 关闭 EventBus
	if db.eventBus != nil {
		if err := db.eventBus.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// 2. 关闭 AOF
	if db.aofHandler != nil {
		if err := db.aofHandler.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
