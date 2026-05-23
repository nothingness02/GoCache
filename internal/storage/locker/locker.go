// Package locker 定义存储引擎的并发控制策略接口
// 不同的锁粒度实现（全局锁、分片锁、无锁等）通过实现 Locker 接口注入到 ShardedEngine 中
package locker

// Locker 定义并发控制策略
// 所有方法返回的 func() 为解锁函数，调用方应通过 defer 释放
type Locker interface {
	// Lock 对指定分片加写锁，返回解锁函数
	Lock(shardIdx int) func()
	// RLock 对指定分片加读锁，返回解锁函数
	RLock(shardIdx int) func()
	// ShardCount 返回总分片数量
	ShardCount() int
}
