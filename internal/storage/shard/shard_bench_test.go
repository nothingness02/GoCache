package shard

import (
	"fmt"
	"testing"
	"time"

	"Flux-KV/internal/storage/core"
)

const benchShardNumKeys = 100000

// makeBenchData 生成基准测试用的 keys 和 values
func makeBenchData(n int) ([]string, [][]byte) {
	keys := make([]string, n)
	vals := make([][]byte, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("bench_key_%010d", i)
		vals[i] = []byte(fmt.Sprintf("bench_val_%010d", i))
	}
	return keys, vals
}

// --- MapShard 直接压测（无锁，单 goroutine） ---

// BenchmarkMapShard_Set_Seq 顺序写入 MapShard
func BenchmarkMapShard_Set_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	s := NewMapShard()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Set(keys[idx], vals[idx], 0)
	}
}

// BenchmarkMapShard_Get_Seq 顺序读取 MapShard（预填充）
func BenchmarkMapShard_Get_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	s := NewMapShard()
	for i := 0; i < benchShardNumKeys; i++ {
		s.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Get(keys[idx])
	}
}

// BenchmarkMapShard_Set_WithTTL_Seq 带 TTL 顺序写入
func BenchmarkMapShard_Set_WithTTL_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	s := NewMapShard()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Set(keys[idx], vals[idx], 60*time.Second)
	}
}

// --- ZeroGCShard 直接压测（无锁，单 goroutine） ---

// BenchmarkZeroGCShard_Set_Seq 顺序写入 ZeroGCShard
func BenchmarkZeroGCShard_Set_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	s := NewZeroGCShard(core.DefaultSharedSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Set(keys[idx], vals[idx], 0)
	}
}

// BenchmarkZeroGCShard_Get_Seq 顺序读取 ZeroGCShard（预填充）
func BenchmarkZeroGCShard_Get_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	s := NewZeroGCShard(core.DefaultSharedSize)
	for i := 0; i < benchShardNumKeys; i++ {
		s.Set(keys[i], vals[i], 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Get(keys[idx])
	}
}

// BenchmarkZeroGCShard_Set_WithTTL_Seq 带 TTL 顺序写入
func BenchmarkZeroGCShard_Set_WithTTL_Seq(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	s := NewZeroGCShard(core.DefaultSharedSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Set(keys[idx], vals[idx], 60*time.Second)
	}
}

// 注：Shard 实现内部均非线程安全（均包含 map），并发压测请在 ShardedEngine 层进行，
// 后者通过 Locker 接口提供完整的并发控制。此处仅保留单 goroutine 的原始吞吐基准。

// --- 驱逐场景压测（ZeroGCShard 容量满后触发驱逐） ---

// BenchmarkZeroGCShard_Eviction 测试 ZeroGCShard 在持续写入小容量下的驱逐性能
func BenchmarkZeroGCShard_Eviction(b *testing.B) {
	keys, vals := makeBenchData(benchShardNumKeys)
	// 使用很小的容量，强制频繁触发驱逐
	s := NewZeroGCShard(64 * 1024) // 64KB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % benchShardNumKeys
		s.Set(keys[idx], vals[idx], 0)
	}
}

// --- 不同 value size 压测 ---

// BenchmarkMapShard_Set_VarValueSize 测试不同 value 大小对 MapShard 写入的影响
func BenchmarkMapShard_Set_VarValueSize(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%db", size), func(b *testing.B) {
			keys := make([]string, benchShardNumKeys)
			vals := make([][]byte, benchShardNumKeys)
			v := make([]byte, size)
			for i := range v {
				v[i] = byte(i % 256)
			}
			for i := 0; i < benchShardNumKeys; i++ {
				keys[i] = fmt.Sprintf("key_%010d", i)
				vals[i] = v
			}
			s := NewMapShard()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx := i % benchShardNumKeys
				s.Set(keys[idx], vals[idx], 0)
			}
		})
	}
}

// BenchmarkZeroGCShard_Set_VarValueSize 测试不同 value 大小对 ZeroGCShard 写入的影响
func BenchmarkZeroGCShard_Set_VarValueSize(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("%db", size), func(b *testing.B) {
			keys := make([]string, benchShardNumKeys)
			vals := make([][]byte, benchShardNumKeys)
			v := make([]byte, size)
			for i := range v {
				v[i] = byte(i % 256)
			}
			for i := 0; i < benchShardNumKeys; i++ {
				keys[i] = fmt.Sprintf("key_%010d", i)
				vals[i] = v
			}
			s := NewZeroGCShard(core.DefaultSharedSize)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx := i % benchShardNumKeys
				s.Set(keys[idx], vals[idx], 0)
			}
		})
	}
}
