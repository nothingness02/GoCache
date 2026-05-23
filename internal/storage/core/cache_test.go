package core

import (
	"strconv"
	"testing"
	"time"
)

// BenchmarkZeroGC_Set_Seq 顺序压测 ZeroGCShard.Set
// 注意：ZeroGCShard 本身非线程安全，并发测试应在 ShardedEngine 层进行
func BenchmarkZeroGC_Set_Seq(b *testing.B) {
	const numKeys = 1000000
	keys := make([]string, numKeys)
	vals := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = "key_" + strconv.Itoa(i)
		vals[i] = []byte("val_" + strconv.Itoa(i))
	}

	shard := NewZeroGCShard(DefaultSharedSize)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx := i % numKeys
		shard.Set(keys[idx], vals[idx], 60*time.Second)
	}
}
