package storage

import (
	"fmt"

	"Flux-KV/internal/config"
	"Flux-KV/internal/storage/core"
	"Flux-KV/internal/storage/locker"
	"Flux-KV/internal/storage/shard"
)

// NewEngine 根据 StorageConfig 自由组合 Locker + Shard 创建存储引擎
// 不再用硬编码名字锁死一种组合，而是按以下维度独立配置：
//   - shard_type:  底层物理存储类型（"map" | "zerogc"）
//   - locker_type: 并发控制策略（"global" | "sharded"）
//   - shard_count: 分片数量（仅 sharded 锁有效）
//   - shard_size:  单个 ZeroGCShard 容量（仅 zerogc 有效，字节）
func NewEngine(cfg *config.Config) (Engine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	sc := cfg.Storage

	// 1. 构建 Locker
	var l locker.Locker
	switch sc.LockerType {
	case "global", "":
		l = locker.NewGlobalRWMutex()
	case "sharded":
		count := sc.ShardCount
		if count <= 0 {
			count = 256
		}
		l = locker.NewShardedRWMutex(count)
	default:
		return nil, fmt.Errorf("unsupported locker type: %q", sc.LockerType)
	}

	// 2. 构建 Shard Factory
	var sf func() shard.Shard
	switch sc.ShardType {
	case "zerogc", "":
		size := sc.ShardSize
		if size <= 0 {
			size = core.DefaultSharedSize
		}
		sf = func() shard.Shard {
			return shard.NewZeroGCShard(uint32(size))
		}
	case "map":
		sf = func() shard.Shard {
			return shard.NewMapShard()
		}
	default:
		return nil, fmt.Errorf("unsupported shard type: %q", sc.ShardType)
	}

	// 3. 组合名称作为引擎类型标识
	engineType := fmt.Sprintf("%s-%s", sc.ShardType, sc.LockerType)

	return NewShardedEngine(l, sf, engineType, cfg)
}
