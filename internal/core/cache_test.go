package core

import (
	"Flux-KV/internal/config"
	"strconv"
	"testing"
	"time"
)

// BenchmarkZeroGC_Set_Parallel 并发压测 Set
func BenchmarkZeroGC_Set_Parallel(b *testing.B) {

	const numKeys = 1000000
	keys := make([]string, numKeys)
	vals := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = "key_" + strconv.Itoa(i)
		vals[i] = []byte("val_" + strconv.Itoa(i))
	}
	// 初始化你的数据库，假设分配 10MB 分片大小
	cfg := &config.Config{
		AOF: config.AOFConfig{Filename: ""},
	}
	db, err := NewMemDB(cfg)
	if err != nil {
		b.Fatal(err)
	}
	// 告诉 b.RunParallel 我们希望在压测前重置计时器
	b.ResetTimer()

	// b.RunParallel 会根据你的 CPU 核心数自动生成大量的并发协程
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 模拟真实的并发写入
			idx := i % numKeys
			// 调用你的 Set，设置 60秒 过期
			db.Set(keys[idx], vals[idx], 60*time.Second)
			i++
		}
	})
}
