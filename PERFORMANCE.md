# Flux-KV 性能基准文档

> 本文档集中存放 Flux-KV 所有模块的真实性能测试数据，作为其他技术文档引用的单一数据源。
>
> **测试环境**: Intel Core i9-14900HX (32 logical cores), Linux 6.6.114.1-microsoft-standard-WSL2, Go 1.24
>
> **测试命令**: `go test ./internal/storage/... -bench=. -benchmem`

---

## 1. 存储引擎层并发压测（ShardedEngine，32 goroutines）

### 1.1 四种核心组合对比

| 组合 | Set (ns/op) | Get (ns/op) | Mixed 80/20 (ns/op) | Set 分配 | Get 分配 |
|---|---|---|---|---|---|
| **ZeroGC + Sharded (256)** | 31.98 | 18.57 | 39.46 | 40 B, 2 allocs | 64 B, 3 allocs |
| **ZeroGC + Global** | 200.8 | 59.14 | — | 40 B, 2 allocs | 64 B, 3 allocs |
| **Map + Sharded (256)** | 29.51 | 14.13 | 27.85 | 88 B, 3 allocs | 40 B, 2 allocs |
| **Map + Global** | 183.7 | 55.13 | — | 89 B, 3 allocs | 40 B, 2 allocs |

> **结论**: Sharded 锁相比 Global 锁，Set 提升 **6.3x**，Get 提升 **3.2x**。MapShard 的 Get 略快于 ZeroGCShard（14ns vs 19ns），但 ZeroGCShard 在 value 较大时内存更优。

### 1.2 分片数量影响（ZeroGC + Sharded）

| ShardCount | Set (ns/op) | Get (ns/op) | 说明 |
|---|---|---|---|
| 1 | 184.8 | 59.18 | 退化到全局锁 |
| 16 | 81.03 | 19.30 | 锁竞争显著降低 |
| 64 | 39.36 | 14.07 | 接近最佳 |
| **256** | **30.18** | **13.82** | **最佳平衡点** |
| 1024 | 29.48 | 36.69 | hash 计算开销上升，Get 倒退 |

> **结论**: 256 是最佳分片数（2^8，位运算取模）。1024 分片因 hash 计算开销和 CPU cache miss，Get 反而倒退到 36ns。

### 1.3 Value Size 影响（Sharded, 256 分片）

| Value Size | ZeroGC Set (ns/op) | Map Set (ns/op) | ZeroGC 内存/次 | Map 内存/次 |
|---|---|---|---|---|
| 64 B | 145.2 | 119.7 | 80 B | 128 B |
| 256 B | 241.3 | 158.1 | 272 B | 320 B |
| 1 KB | 418.1 | 296.8 | 1041 B | 1090 B |
| 4 KB | 1578 | 821.6 | 4117 B | 4165 B |
| 16 KB | — | 3335 | — | 16463 B |

> **结论**: ZeroGCShard 在小 value（<1KB）下分配更少，但大 value（>1KB）时 MapShard 更快。ZeroGCShard 的循环缓冲区在 value 过大时频繁触发驱逐和内存拷贝。

### 1.4 带 TTL 并发写入

| 组合 | Set with TTL (ns/op) | 分配 |
|---|---|---|
| ZeroGC + Sharded (256) | 24.84 | 40 B, 2 allocs |
| Map + Sharded (256) | 31.40 | 88 B, 3 allocs |

### 1.5 顺序写入（单 goroutine，对比并发开销）

| 组合 | Set Seq (ns/op) | 分配 |
|---|---|---|
| ZeroGC + Sharded (256) | 139.5 | 40 B, 2 allocs |
| Map + Sharded (256) | 113.4 | 88 B, 3 allocs |

---

## 2. Shard 原始层压测（单 goroutine，无锁）

| Shard 类型 | Set (ns/op) | Get (ns/op) | 带 TTL Set (ns/op) | Eviction (ns/op) | 说明 |
|---|---|---|---|---|---|
| MapShard | 43.22 | 14.43 | 93.92 | — | 标准 map，48 B/op 分配 |
| ZeroGCShard | 73.14 | 103.0 | 73.17 | 68.75 | 预分配池，**0 B/op** |

### 2.1 不同 Value Size（MapShard）

| Value Size | Set (ns/op) | 每次分配 |
|---|---|---|
| 64 B ~ 16 KB | 40~43 ns/op | 固定 48 B |

### 2.2 不同 Value Size（ZeroGCShard）

| Value Size | Set (ns/op) | 每次分配 |
|---|---|---|
| 64 B | 69.08 | **0 B** |
| 256 B | 75.29 | **0 B** |
| 1 KB | 83.54 | **0 B** |
| 4 KB | 150.7 | **0 B** |

> **结论**: ZeroGCShard 真正的优势是 **0 堆分配**，对 GC 压力最小。单次操作比 MapShard 慢约 30ns，但在高并发场景下 GC 收益更大。

---

## 3. 核心优化效果对比

| 优化项 | 优化前 | 优化后 | 提升 |
|---|---|---|---|
| hash() 分配 | 堆分配 fnv.New64a() | 栈上 inline FNV-1a | 消除每次 hash 的接口分配 |
| Get() 锁竞争 | RLock → Lock 升级 | 纯 RLock 读取 | 消除写锁竞争 |
| AOF 写入 | 单条 JSON + syscall | 批量 100 条/10ms flush | syscall 次数减少 100x |
| time.After | 每次新建 Timer | 复用 timer / 非阻塞 drop | 消除 timer 堆分配 |
| gRPC label | 运行时 string | 编译期 const | 消除每次请求 string 分配 |

---

## 4. Raft 性能基准

| 指标 | 数值 | 说明 |
|---|---|---|
| 心跳间隔 | 100 ms | Leader 发送 AppendEntries |
| 选举超时 | 500~1000 ms | 随机化，避免活锁 |
| applyLoop 轮询周期 | 10 ms | 日志应用到状态机 |
| ReadIndex 超时 | 5 s | 默认线性化读等待时间 |
| WAL 持久化格式 | JSON | `raft.state` + `raft.log`，原子 rename |
| 快照阈值 | 10000 条 | `lastApplied - lastSnapshotIndex > 10000` |
| 快照格式 | 二进制 | `[Index:8][Term:8][DataLen:4][Data]` |

---

*文档版本: v0.2 | 更新日期: 2026-05-23*
