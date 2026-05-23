package test

import (
	"fmt"
	"testing"
	"time"

	"Flux-KV/internal/config"
	"Flux-KV/internal/storage"
)

const benchNumKeys = 100000

// makeBenchData 生成基准测试用的 keys 和 values
func makeBenchData(n int) ([]string, []string) {
	keys := make([]string, n)
	vals := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("bench_key_%010d", i)
		vals[i] = fmt.Sprintf("bench_val_%010d", i)
	}
	return keys, vals
}

// newEngineFromConfig 通过配置创建引擎的辅助函数
func newEngineFromConfig(shardType, lockerType string, shardCount int) storage.Engine {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			ShardType:  shardType,
			LockerType: lockerType,
			ShardCount: shardCount,
			ShardSize:  10 * 1024 * 1024, // 10MB
		},
	}
	eng, err := storage.NewEngine(cfg)
	if err != nil {
		panic(err)
	}
	return eng
}

// --- 四种核心组合并发压测 ---

// BenchmarkEngine_zerogc_sharded_Set_Parallel ZeroGC + ShardedRWMutex 并发写入
func BenchmarkEngine_zerogc_sharded_Set_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "sharded", 256)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Set(keys[idx], vals[idx], 0)
			i++
		}
	})
}

// BenchmarkEngine_zerogc_global_Set_Parallel ZeroGC + GlobalRWMutex 并发写入
func BenchmarkEngine_zerogc_global_Set_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "global", 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Set(keys[idx], vals[idx], 0)
			i++
		}
	})
}

// BenchmarkEngine_map_sharded_Set_Parallel MapShard + ShardedRWMutex 并发写入
func BenchmarkEngine_map_sharded_Set_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "sharded", 256)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Set(keys[idx], vals[idx], 0)
			i++
		}
	})
}

// BenchmarkEngine_map_global_Set_Parallel MapShard + GlobalRWMutex 并发写入
func BenchmarkEngine_map_global_Set_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "global", 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Set(keys[idx], vals[idx], 0)
			i++
		}
	})
}

// --- 四种核心组合并发读取 ---

func BenchmarkEngine_zerogc_sharded_Get_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "sharded", 256)
	for i := 0; i < benchNumKeys; i++ {
		eng.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Get(keys[idx])
			i++
		}
	})
}

func BenchmarkEngine_zerogc_global_Get_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "global", 0)
	for i := 0; i < benchNumKeys; i++ {
		eng.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Get(keys[idx])
			i++
		}
	})
}

func BenchmarkEngine_map_sharded_Get_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "sharded", 256)
	for i := 0; i < benchNumKeys; i++ {
		eng.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Get(keys[idx])
			i++
		}
	})
}

func BenchmarkEngine_map_global_Get_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "global", 0)
	for i := 0; i < benchNumKeys; i++ {
		eng.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Get(keys[idx])
			i++
		}
	})
}

// --- 混合读写（80% 读 / 20% 写）---

func BenchmarkEngine_zerogc_sharded_Mixed80_20_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "sharded", 256)
	for i := 0; i < benchNumKeys; i++ {
		eng.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			if i%5 == 0 {
				eng.Set(keys[idx], vals[idx], 0)
			} else {
				eng.Get(keys[idx])
			}
			i++
		}
	})
}

func BenchmarkEngine_map_sharded_Mixed80_20_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "sharded", 256)
	for i := 0; i < benchNumKeys; i++ {
		eng.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			if i%5 == 0 {
				eng.Set(keys[idx], vals[idx], 0)
			} else {
				eng.Get(keys[idx])
			}
			i++
		}
	})
}

// --- 分片数量对比（固定 zerogc + sharded，只改 shard_count） ---

func BenchmarkEngine_zerogc_sharded_Set_VarShardCount(b *testing.B) {
	shardCounts := []int{1, 16, 64, 256, 1024}
	for _, count := range shardCounts {
		b.Run(fmt.Sprintf("%d", count), func(b *testing.B) {
			keys, vals := makeBenchData(benchNumKeys)
			eng := newEngineFromConfig("zerogc", "sharded", count)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					idx := i % benchNumKeys
					eng.Set(keys[idx], vals[idx], 0)
					i++
				}
			})
		})
	}
}

func BenchmarkEngine_zerogc_sharded_Get_VarShardCount(b *testing.B) {
	shardCounts := []int{1, 16, 64, 256, 1024}
	for _, count := range shardCounts {
		b.Run(fmt.Sprintf("%d", count), func(b *testing.B) {
			keys, vals := makeBenchData(benchNumKeys)
			eng := newEngineFromConfig("zerogc", "sharded", count)
			for i := 0; i < benchNumKeys; i++ {
				eng.Set(keys[i], vals[i], 0)
			}
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					idx := i % benchNumKeys
					eng.Get(keys[idx])
					i++
				}
			})
		})
	}
}

// --- 不同 value size 压测 ---

func BenchmarkEngine_zerogc_sharded_Set_VarValueSize(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%db", size), func(b *testing.B) {
			keys := make([]string, benchNumKeys)
			vals := make([]string, benchNumKeys)
			v := make([]byte, size)
			for i := range v {
				v[i] = byte(i % 256)
			}
			valStr := string(v)
			for i := 0; i < benchNumKeys; i++ {
				keys[i] = fmt.Sprintf("key_%010d", i)
				vals[i] = valStr
			}
			eng := newEngineFromConfig("zerogc", "sharded", 256)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx := i % benchNumKeys
				eng.Set(keys[idx], vals[idx], 0)
			}
		})
	}
}

func BenchmarkEngine_map_sharded_Set_VarValueSize(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%db", size), func(b *testing.B) {
			keys := make([]string, benchNumKeys)
			vals := make([]string, benchNumKeys)
			v := make([]byte, size)
			for i := range v {
				v[i] = byte(i % 256)
			}
			valStr := string(v)
			for i := 0; i < benchNumKeys; i++ {
				keys[i] = fmt.Sprintf("key_%010d", i)
				vals[i] = valStr
			}
			eng := newEngineFromConfig("map", "sharded", 256)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx := i % benchNumKeys
				eng.Set(keys[idx], vals[idx], 0)
			}
		})
	}
}

// --- 带 TTL 压测 ---

func BenchmarkEngine_zerogc_sharded_Set_WithTTL_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "sharded", 256)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Set(keys[idx], vals[idx], 60*time.Second)
			i++
		}
	})
}

func BenchmarkEngine_map_sharded_Set_WithTTL_Parallel(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "sharded", 256)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % benchNumKeys
			eng.Set(keys[idx], vals[idx], 60*time.Second)
			i++
		}
	})
}

// --- 顺序写入（单 goroutine，对比并发开销） ---

func BenchmarkEngine_zerogc_sharded_Set_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("zerogc", "sharded", 256)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchNumKeys
		eng.Set(keys[idx], vals[idx], 0)
	}
}

func BenchmarkEngine_map_sharded_Set_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchNumKeys)
	eng := newEngineFromConfig("map", "sharded", 256)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchNumKeys
		eng.Set(keys[idx], vals[idx], 0)
	}
}
