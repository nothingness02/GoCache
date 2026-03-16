package core

import (
	"Flux-KV/internal/config"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// BenchmarkMemDB_Set_Parallel 测试高并发写入性能
// 这是验证“分片锁”是否有效的核心测试
// 如果分片生效，多核 CPU 下的吞吐量应显著高于单锁版本
func BenchmarkMemDB_Set_Parallel(b *testing.B) {
	// 关闭 AOF 以测试纯内存性能
	cfg := &config.Config{
		AOF: config.AOFConfig{
			Filename: "",
		},
	}
	db, _ := NewMemDB(cfg)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// 让每个 Goroutine 拥有独立的随机数生成器，避免 math/rand 全局锁成为瓶颈
		// 这样才能测出 DB 锁的性能，而不是随机数生成器的性能
		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		for pb.Next() {
			// 生成随机 Key，尽可能打散到不同分片
			key := fmt.Sprintf("key-%d-%d", r.Int(), r.Int())
			db.Set(key, "value", 0)
		}
	})
}

// BenchmarkMemDB_Get_Parallel 测试高并发读取性能
// 所有的 Get 操作使用 RLock，这在分片锁模式下并发度极高
func BenchmarkMemDB_Get_Parallel(b *testing.B) {
	cfg := &config.Config{
		AOF: config.AOFConfig{
			Filename: "",
		},
	}
	db, _ := NewMemDB(cfg)

	// 预热 10万 条数据
	const dataCount = 100000
	for i := 0; i < dataCount; i++ {
		key := fmt.Sprintf("key-%d", i)
		db.Set(key, "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			// 随机读取
			k := r.Intn(dataCount)
			key := fmt.Sprintf("key-%d", k)
			db.Get(key)
		}
	})
}

// BenchmarkMemDB_Mixed_Parallel 混合读写测试 (80% 读, 20% 写)
// 模拟真实场景
func BenchmarkMemDB_Mixed_Parallel(b *testing.B) {
	cfg := &config.Config{
		AOF: config.AOFConfig{
			Filename: "",
		},
	}
	db, _ := NewMemDB(cfg)

	const dataCount = 100000
	for i := 0; i < dataCount; i++ {
		key := fmt.Sprintf("key-%d", i)
		db.Set(key, "value", 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			// 20% 概率写，80% 概率读
			if r.Intn(100) < 20 {
				key := fmt.Sprintf("key-%d", r.Intn(dataCount))
				db.Set(key, "new-value", 0)
			} else {
				key := fmt.Sprintf("key-%d", r.Intn(dataCount))
				db.Get(key)
			}
		}
	})
}
