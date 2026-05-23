# Flux-KV 技术架构文档

> 本文档旨在清晰梳理 Flux-KV 项目的整体结构、模块职责、数据流和交互关系，帮助你快速理解系统框架。

---

## 1. 项目概述

**Flux-KV** 是一个基于 Go 语言实现的高性能分布式键值存储系统，采用 **CP/AP 双模式**架构设计，可根据业务场景在强一致性和高可用性之间灵活切换。

### 核心特性

| 特性 | 说明 |
|------|------|
| **双模式存储** | CP 模式基于 Raft 共识算法保证强一致性；AP 模式直接本地存储保证高性能 |
| **分片并发** | MemDB 采用 256 分片 + FNV-1a 哈希，大幅降低锁竞争 |
| **AOF 持久化** | JSON 行格式追加写入，支持异步刷盘和启动恢复 |
| **CDC 事件流** | 写操作通过 RabbitMQ 发布变更数据捕获事件 |
| **服务发现** | 基于 Etcd 的注册/发现机制，支持动态节点上下线 |
| **客户端弹性** | 内置连接池、健康检查、熔断器、指数退避重试 |
| **可观测性** | 集成 Prometheus 指标、Jaeger 链路追踪、Zap 结构化日志 |

### 技术栈

- **语言**: Go 1.24
- **RPC 框架**: gRPC + Protocol Buffers
- **HTTP 网关**: Gin
- **服务注册**: Etcd
- **消息队列**: RabbitMQ
- **指标**: Prometheus
- **链路追踪**: OpenTelemetry + Jaeger
- **日志**: Uber Zap

---

## 2. 目录结构总览

```
Flux-KV/
├── api/proto/                  # gRPC Protobuf 定义
│   ├── kv.proto               # KV 服务接口
│   └── raft/raft.proto        # Raft 内部通信接口
│
├── cmd/                        # 可执行程序入口
│   ├── server/main.go         # KV 存储节点服务
│   ├── gateway/main.go        # HTTP API 网关
│   ├── client/main.go         # 交互式 CLI 客户端
│   ├── benchmark/main.go      # 压力测试工具
│   ├── cdc_consumer/main.go   # CDC 事件消费者
│   └── prometheus-sd/main.go  # Prometheus 服务发现适配器
│
├── configs/
│   ├── config.yaml            # 主配置文件
│   └── prometheus.yaml        # Prometheus 抓取配置
│
├── docs/                       # 技术文档
│   ├── API.md                 # HTTP API 参考
│   ├── DOCKER.md              # Docker 部署指南
│   ├── ARCHITECTURE.md        # 本文档
│   └── superpowers/           # 技术深度文章与 Q&A
│
├── internal/                   # 内部实现（不对外暴露）
│   ├── config/                # 配置管理（Viper）
│   ├── event/                 # RabbitMQ 事件总线（CDC）
│   ├── network/
│   │   ├── gateway/handler/   # HTTP 请求处理器
│   │   ├── gateway/router/    # Gin 路由与中间件链
│   │   └── protocol/          # TCP 文本协议服务器
│   ├── app/                    # 应用层 UseCase + NodeMeta + Migrator
│   ├── raft/                   # Raft 共识实现
│   │   ├── types.go            # 核心数据结构 + ApplyStorage 接口
│   │   ├── node.go             # 选举、日志复制、心跳
│   │   ├── transport.go        # gRPC 传输实现
│   │   ├── wal.go              # WAL 持久化
│   │   ├── snapshot.go         # 快照压缩
│   │   └── apply.go            # 状态机应用循环
│   ├── transport/
│   │   └── grpc/               # gRPC KV Server 实现
│   ├── server/                # gRPC Server 生命周期管理
│   ├── service/               # gRPC 服务实现（KVService）
│   └── storage/               # 存储层
│       ├── core/               # ZeroGCShard / MapShard 实现
│       ├── shard/              # Shard 接口 + 两种实现
│       ├── locker/             # Locker 接口（Global/Sharded RWMutex）
│       ├── aof/                # AOF 批量持久化
│       ├── engine.go           # Engine 接口
│       ├── factory.go          # 引擎工厂（按维度组合）
│       ├── sharded_engine.go   # 统一分片引擎
│       ├── ap_storage.go       # AP 模式包装器
│       └── cp_storage.go       # CP 模式包装器（+Raft）
│
├── pkg/                        # 公共包（可被外部导入）
│   ├── consistent/            # 一致性哈希
│   ├── logger/                # Zap 日志封装
│   ├── metrics/               # Prometheus 指标定义
│   ├── middleware/            # Gin 中间件（限流、熔断、日志）
│   ├── resilience/             # 弹性组件（限流器、熔断器）
│   └── network/
│       ├── client/             # gRPC 客户端 SDK
│       │   ├── balancer/       # 负载均衡器
│       │   ├── picker/         # 一致性哈希选择器
│       │   ├── resolver/       # Etcd 服务解析器
│       │   └── interceptors/   # 客户端拦截器（重试、熔断）
│       ├── discovery/         # Etcd 服务注册与发现
│       └── tracer/            # OpenTelemetry 链路追踪
│
├── scripts/                    # 运维脚本
│   ├── docker_start.sh
│   ├── docker_stop.sh
│   ├── docker_clean.sh
│   ├── test_gateway.sh
│   ├── my_load_test.sh
│   └── chaos_test.py
│
├── test/                       # 集成测试与测试基础设施
│   ├── mock_server.go         # Mock gRPC 服务器
│   ├── raft_mock.go           # Raft 测试集群辅助工具
│   ├── *_test.go              # 各模块测试用例
│
├── docker-compose.yaml         # 完整集群编排
├── Dockerfile.server           # Server 镜像
├── Dockerfile.gateway          # Gateway 镜像
├── Dockerfile.consumer         # CDC Consumer 镜像
├── Dockerfile.prometheus-sd    # Prometheus SD 镜像
├── go.mod                      # 依赖管理
└── README.md                   # 项目简介
```

---

## 3. 系统架构

### 3.1 整体架构图

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Client Layer                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ CLI Client   │  │ HTTP Client  │  │ Benchmark    │              │
│  │ cmd/client   │  │ (curl/httpie)│  │ cmd/benchmark│              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
└─────────┼─────────────────┼─────────────────┼──────────────────────┘
          │                 │                 │
          │  gRPC           │  HTTP           │  gRPC
          ▼                 ▼                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        Gateway Layer                                │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  HTTP API (Gin)  ──▶  RateLimit ──▶  CircuitBreaker         │  │
│  │     │                                               │         │  │
│  │     ▼                                               ▼         │  │
│  │  HandleSet/HandleGet/HandleDel  ──▶  Client SDK (gRPC)      │  │
│  │     │                                                        │  │
│  │     ▼                                                        │  │
│  │  singleflight.Group (并发请求去重)                             │  │
│  └──────────────────────────────────────────────────────────────┘  │
│  Port: 8080                                                         │
└─────────────────────────────────────────────────────────────────────┘
          │
          │  gRPC (with ConnPool + HealthCheck + Breaker + Retry)
          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Storage Node Layer                             │
│                                                                     │
│  ┌─────────────────────────┐    ┌─────────────────────────┐        │
│  │      CP Mode (3 nodes)  │    │      AP Mode (2 nodes)  │        │
│  │  ┌───────────────────┐  │    │  ┌───────────────────┐  │        │
│  │  │  CPStorage        │  │    │  │  APStorage        │  │        │
│  │  │  ├─ Engine        │  │    │  │  ├─ Engine        │  │        │
│  │  │  └─ RaftNode      │  │    │  │  └─ (direct)      │  │        │
│  │  │    ├─ GRPCTransport│ │    │  └───────────────────┘  │        │
│  │  │    ├─ Log          │ │    │                         │        │
│  │  │    └─ ApplyLoop    │ │    │  Ports: 50055, 50056    │        │
│  │  └───────────────────┘  │    └─────────────────────────┘        │
│  │  Ports: 50052-50054     │                                        │
│  │  Raft: 12001-12003      │                                        │
│  └─────────────────────────┘                                        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
          │
          │  Write-through
          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     Infrastructure Layer                            │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐    │
│  │  Etcd      │  │  RabbitMQ  │  │  Jaeger    │  │ Prometheus │    │
│  │ (Service   │  │ (CDC       │  │ (Tracing)  │  │ (Metrics)  │    │
│  │  Discovery)│  │  Events)   │  │            │  │            │    │
│  │  Port:2379 │  │  Port:5672 │  │  Port:4317 │  │  Port:9090 │    │
│  └────────────┘  └────────────┘  └────────────┘  └────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

### 3.2 存储层架构（组合模型）

存储引擎由三个独立维度组合而成，不再使用硬编码名称：

```
ShardedEngine = Locker（并发控制） + []Shard（物理存储）
```

**维度 1: Shard 类型**
- `zerogc`: 预分配字节池，0 堆分配，循环缓冲区驱逐
- `map`: 标准 `map[string][]byte`，Go runtime 优化

**维度 2: Locker 类型**
- `sharded`: `N` 把 `sync.RWMutex`，按 hash 取模选择锁
- `global`: 单把 `sync.RWMutex`，全引擎串行化

**维度 3: 分片数**
- 仅 `sharded` 锁有效，默认 256（2^8，位运算取模）
- 1024 分片在写入上略快，但读取因 hash 开销反而倒退

**性能基准（32 goroutines 并发）:**

| 组合 | Set | Get | 说明 |
|---|---|---|---|
| zerogc + sharded (256) | 32 ns | 19 ns | **默认推荐**，平衡 GC 和速度 |
| map + sharded (256) | 30 ns | 14 ns | 纯速度最优，但每 op 多 48 B 分配 |
| zerogc + global | 201 ns | 59 ns | 锁竞争严重，不推荐 |
| map + global | 184 ns | 55 ns | 锁竞争严重，不推荐 |

> 完整 benchmark 见 [PERFORMANCE.md](../PERFORMANCE.md)

### 3.5 Raft 生产化架构

Raft 实现已从纯内存演进为生产可用：

**持久化层:**
- `wal.go`: FileWAL 将 `term + votedFor` 和 `log[]` 分别存入 `raft.state` / `raft.log`，使用 write-to-temp + rename 原子写入。
- `snapshot.go`: 当 `lastApplied - lastSnapshotIndex > 10000` 时触发二进制快照，截断 WAL。

**传输层:**
- Raft gRPC Service 与 KV gRPC Service 共享端口（默认 50052），`FLUX_RAFT_PEERS` 直接指向业务端口。

**线性化读:**
- Leader 收到读请求后 append no-op，等待 majority commit（`ReadIndex`），再执行本地引擎读取。

**状态恢复顺序（重启时）:**
1. 加载最新快照 → `storage.Restore()` → 恢复状态机
2. 加载 WAL → 恢复 `term / votedFor / log[]`
3. 重放快照点之后的日志

### 3.6 运维与可观测性

**Readiness Probe:**
- Server `/ready` 检查 Etcd 连通性 + 存储引擎初始化状态
- Gateway `/ready` 检查后端 Etcd 可达性
- 未就绪返回 503，Kubernetes 可据此控制流量

**Request-ID 传播:**
- Gateway 生成/提取 `x-request-id`，通过 gRPC metadata 透传到后端节点
- 全链路日志携带 `request_id` 字段，便于分布式追踪

**日志采样:**
- Zap 支持 `SamplingConfig`（初始 100 条 + 之后每 100 条采样 1 条）
- 配置项: `log.sampling.initial`, `log.sampling.thereafter`

**AOF 批量写入:**
- 积累 100 条命令或 10ms 后统一 flush，减少 syscall
- 配置项: `aof.batch_size`, `aof.flush_interval_ms`

---

## 4. 核心模块详解

### 4.1 存储层 (internal/storage/)

存储层采用 **策略模式 + 适配器模式**，通过统一的 `Engine` 接口屏蔽底层差异。

#### Engine 接口

```go
// internal/storage/engine.go
type Engine interface {
    Get(key string) (string, bool)
    Set(key string, value string, ttl time.Duration) error
    Delete(key string) error
    Stats() EngineStats
    Close() error
}
```

#### 架构层次

```
┌──────────────────────────────────────────────┐
│              storage.Engine                   │  ← 统一接口
├──────────────────┬───────────────────────────┤
│   APStorage      │      CPStorage            │  ← 策略模式
│  (直接透传)       │  (Engine + RaftNode)      │
├──────────────────┴───────────────────────────┤
│   MemDBAdapter       │    SimpleMapEngine     │  ← 引擎实现
│  (Adapter Pattern)   │    (map+RWMutex)       │
├──────────────────────┴───────────────────────┤
│   MemDB (core.MemDB)                        │  ← 核心内存存储
│   ├─ 256 shards (ZeroGCShard)               │
│   ├─ AOF Handler                            │
│   └─ EventBus (RabbitMQ)                    │
└──────────────────────────────────────────────┘
```

#### MemDB 分片设计

**文件**: `internal/storage/core/mem_db.go`

- 256 个分片，FNV-1a 哈希路由
- 每个分片独立的 `sync.RWMutex`
- 细粒度锁，大并发下锁竞争极低
- 支持 TTL 过期（惰性删除）

```go
const ShardCount = 256

func (db *MemDB) getShard(key string) *shard {
    hash := fnv32(key)
    return db.shards[hash%ShardCount]
}
```

#### ZeroGCShard

**文件**: `internal/storage/core/cache.go`

自定义环形缓冲区实现，特点：
- 预分配固定字节池（`dataPool`），无运行时 GC 压力
- FNV-1a 64-bit 哈希定位
- 写满时自动驱逐最旧数据
- 适合作为高频读写的缓存底层

#### AOF 持久化

**文件**: `internal/storage/aof/aof.go`

```
AOF 文件格式（JSON 行）:
{"type":"set","key":"name","value":"flux"}
{"type":"del","key":"name"}
```

- `AsyncWrite`: 非阻塞发送到 channel，后台 goroutine 批量写入
- `SyncWrite`: 加锁直接写入文件（用于恢复时）
- `ReadAll`: 启动时逐行读取并回放命令
- `Close`: 先 `close(stopCh)` 停止后台 goroutine，等待 `wg.Wait()`，再刷盘关闭文件

#### 工厂函数

**文件**: `internal/storage/factory.go`

```go
func NewEngine(engineType string, cfg *config.Config) (Engine, error)
// 支持: "memdb" (默认), "simplemap"
```

---

### 4.2 Raft 共识层 (internal/raft/)

完整的 Raft 算法实现，支持 Leader 选举、日志复制和安全关闭。

#### 核心数据结构

**文件**: `internal/raft/types.go`

```go
type RaftNode struct {
    // 持久化状态
    currentTerm uint64
    votedFor    string
    log         []LogEntry

    // 易失状态
    state       NodeState      // Follower / Candidate / Leader
    commitIndex uint64
    lastApplied uint64

    // Leader 状态
    nextIndex   map[string]uint64
    matchIndex  map[string]uint64

    // 控制通道
    stopCh     chan struct{}
    appendCh   chan struct{}  // 收到心跳时触发
    electionCh chan struct{}  // 选举超时触发
}
```

#### 状态机转换

```
                    ┌─────────────┐
        ┌───────────│  Follower   │◄────────────────┐
        │           └──────┬──────┘                 │
        │                  │ election timeout        │
        │ higher term      ▼                         │ heartbeat
        │ heartbeat  ┌─────────────┐                 │
        └────────────│  Candidate  │                 │
                     └──────┬──────┘                 │
                            │ majority votes         │
                            ▼                        │
                     ┌─────────────┐                 │
                     │   Leader    │─────────────────┘
                     └─────────────┘  heartbeat
```

#### 关键流程

| 方法 | 文件 | 说明 |
|------|------|------|
| `run()` | `node.go:129` | 主循环，根据状态分发到对应处理器 |
| `runFollower()` | `node.go:152` | 监听 appendCh 和 election timer |
| `becomeCandidate()` | `node.go:179` | 自增 term，向所有 peer 请求投票，用 `select` 监听 appendCh/timer/doneCh |
| `becomeLeader()` | `node.go:254` | 初始化 nextIndex/matchIndex，立即发送心跳 |
| `sendHeartbeats()` | `node.go:295` | Leader 定期发送 AppendEntries（含日志条目） |
| `handleRequestVote()` | `node.go:400` | 处理投票请求，检查 term 和日志新鲜度 |
| `handleAppendEntries()` | `node.go:445` | 处理心跳/日志复制，检查 prevLog 匹配，截断冲突日志 |
| `applyLoop()` | `apply.go:10` | 10ms ticker，将已提交日志应用到存储引擎 |

#### gRPC Transport

**文件**: `internal/raft/transport.go`

- `GRPCTransport` 实现了 `Transport` 接口
- 客户端连接缓存（`clients` map），复用 `grpc.ClientConn`
- 连接建立时使用 `grpc.WithBlock()` + 3 秒超时
- 请求失败时自动移除并重建连接
- `RaftGRPCServer` 处理接收到的 RequestVote 和 AppendEntries RPC

---

### 4.3 客户端层 (pkg/network/client/)

工业级 gRPC 客户端 SDK，内置多种弹性模式。

#### 整体结构

```go
// pkg/network/client/client.go
type Client struct {
    pool        *ConnPool           // 连接生命周期管理
    np          *nodePool           // 节点选择
    hc          *HealthChecker      // 主动健康探测
    breakers    *BreakerManager     // 熔断器
    retryPolicy RetryPolicy         // 重试策略
}
```

#### 组件详解

**ConnPool** (`pool.go`)
- 按地址维护 `PooledConn`，复用 gRPC 连接
- 支持最大空闲时间（`maxIdle`）和最大连接年龄（`maxAge`）
- 后台 cleanup goroutine 定期扫描并关闭过期连接
- `sync.Once` 保护 `Close()` 避免重复关闭 panic

**HealthChecker** (`health.go`)
- 状态机：`Unknown -> Probing -> Healthy/Unhealthy`
- 可配置阈值：`unhealthyThreshold`（默认 2 次失败）、`healthyThreshold`（默认 1 次成功）
- 支持复用 ConnPool 连接（`SetConnGetter`）
- `sync.Once` 保护 `Stop()`

**CircuitBreaker** (`breaker.go`)
- 三态：`Closed`（正常）-> `Open`（熔断）-> `HalfOpen`（探测）
- 基于连续失败次数触发，基于成功次数恢复
- 每个后端地址独立一个熔断器

**Retry** (`retry.go`)
- 指数退避 + 随机抖动
- 可配置最大重试次数、最大延迟
- 基于 gRPC 状态码过滤可重试错误
- 支持 `context.Context` 取消

**nodePool** (`node_pool.go`)
- AP 节点：一致性哈希（`consistent.Map`，20 虚拟节点/物理节点）
- CP 节点：按 Group 维护，缓存 Leader 地址
- 健康感知：不健康节点触发 fallback

#### 客户端初始化流程

```go
// 1. 创建 Discovery 并 Watch Etcd
d, _ := discovery.NewDiscovery(endpoints)

// 2. 创建 Client（自动订阅节点变化）
cli, _ := client.NewClient(d, "kv-service",
    client.WithPoolConfig(...),
    client.WithHealthCheckConfig(...),
    client.WithRetryPolicy(...),
    client.WithBreakerConfig(...),
)

// 3. 使用
cli.Set(ctx, key, value)        // 默认 AP 模式
cli.SetWithMode(ctx, key, value, client.ModeCP)  // CP 模式
```

---

### 4.4 网关层 (internal/network/gateway/)

#### 路由结构

**文件**: `internal/network/gateway/router/router.go`

```
GET  /health                  → healthHandler.Ping

POST   /api/v1/kv            → kvHandler.HandleSet    (with CircuitBreaker)
GET    /api/v1/kv            → kvHandler.HandleGet    (with CircuitBreaker)
DELETE /api/v1/kv            → kvHandler.HandleDel    (with CircuitBreaker)

GET /admin/nodes             → adminHandler.ListNodes
GET /admin/nodes/:addr/status → adminHandler.NodeStatus
GET /admin/stats             → adminHandler.ClusterStats
```

#### 中间件链（按执行顺序）

1. `gin.Recovery()` — panic 恢复
2. `otelgin.Middleware()` — OpenTelemetry 链路追踪
3. `middleware.AccessLog()` — 访问日志
4. `middleware.GlobalRateLimiter(1000, 2000)` — 全局令牌桶限流
5. `middleware.CircuitBreaker("kv-service")` — API 路由熔断保护

#### SingleFlight 请求去重

**文件**: `internal/network/gateway/handler/kv.go:76`

GET 请求使用 `singleflight.Group` 合并同一 key 的并发请求：
- 第一个请求执行实际查询
- 后续相同 key 的请求等待结果并复用
- 删除操作会调用 `Forget(key)` 清除缓存

#### 模式选择

通过 URL 查询参数 `?mode=cp` 或 `?mode=ap`（默认 AP）控制路由策略。

---

### 4.5 服务发现 (pkg/network/discovery/)

#### 注册 (register.go)

```go
type Registry struct {
    cli     *clientv3.Client
    leaseID clientv3.LeaseID
    cancel  context.CancelFunc  // 用于停止旧 keepalive goroutine
    mu      sync.Mutex
}
```

- `Register()`: 申请 Etcd lease -> Put key with lease -> 启动 keepalive goroutine
- 重新注册时先 `cancel()` 旧的 keepalive，再创建新的
- `Close()`: 撤销 lease（Etcd 自动删除 key）-> 关闭客户端

#### 发现 (discovery.go)

- `WatchService(prefix, addCallback, removeCallback)`: 监听 Etcd prefix
- 新 key 触发 `addCallback`
- key 删除触发 `removeCallback`
- NodeInfo 包含：NodeID, Addr, GroupID, Mode, IsLeader, EngineType

---

### 4.6 协议层

#### gRPC 服务定义

**文件**: `api/proto/kv.proto`

```protobuf
service KVService {
  rpc Set(SetRequest) returns (SetResponse);
  rpc Get(GetRequest) returns (GetResponse);
  rpc Del(DelRequest) returns (DelResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc RaftInfo(RaftInfoRequest) returns (RaftInfoResponse);
}
```

**文件**: `api/proto/raft/raft.proto`

```protobuf
service RaftService {
  rpc RequestVote(RequestVoteRequest) returns (RequestVoteResponse);
  rpc AppendEntries(AppendEntriesRequest) returns (AppendEntriesResponse);
}
```

#### TCP 文本协议

**文件**: `internal/network/protocol/`

简单的文本协议，用于非 gRPC 客户端：
- 编码：`[4字节长度][Payload]`
- 支持命令：`SET key value`, `GET key`, `DEL key`
- 返回：`OK` 或值或 `(nil)`

---

### 4.7 可观测性

#### 日志 (pkg/logger/logger.go)

- 基于 Uber Zap
- 全局单例 `Log *zap.Logger`
- 支持 console / json 编码
- Server 初始化时自动配置

#### 指标 (pkg/metrics/metrics.go)

Prometheus 指标定义：

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `gateway_requests_total` | Counter | 网关请求数（按 mode, status 标签） |
| `gateway_backend_nodes` | Gauge | 后端节点数量（按 mode 标签） |
| `grpc_requests_total` | Counter | gRPC 请求数 |
| `kv_requests_total` | Counter | KV 操作数 |
| `kv_set_total` | Counter | Set 操作数 |
| `kv_get_total` | Counter | Get 操作数 |
| `raft_role` | Gauge | Raft 节点角色（0=Follower, 1=Candidate, 2=Leader） |
| `raft_commit_index` | Gauge | Raft 提交索引 |
| `raft_healthy_nodes` | Gauge | Raft 健康节点数 |
| `engine_memory_bytes` | Gauge | 引擎内存占用 |

#### 链路追踪 (pkg/network/tracer/)

- OpenTelemetry SDK 初始化
- Jaeger OTLP gRPC exporter
- Gateway 自动注入 tracing middleware
- Client gRPC 通过 `otelgrpc.NewClientHandler()` 注入

---

### 4.8 配置系统 (internal/config/)

**文件**: `internal/config/config.go`

#### 配置结构

```go
type Config struct {
    Server   ServerConfig   // port, mode
    Storage  StorageConfig  // engine type
    Raft     RaftConfig     // enabled, node_id, peers, bind_addr
    AOF      AOFConfig      // filename, append_fsync
    Etcd     EtcdConfig     // endpoints
    RabbitMQ RabbitMQConfig // url
    Jaeger   JaegerConfig   // endpoint
    Pprof    PprofConfig    // enabled, port
    CDC      CDCConfig      // exchange, queue, log_path
    Log      LogConfig      // level, encoding
}
```

#### 加载优先级

```
环境变量 (FLUX_*) > configs/config.yaml > 代码默认值
```

- 环境变量自动映射：`FLUX_SERVER_PORT` -> `server.port`
- 容器化部署时自动检测服务 IP（`FLUX_POD_IP` -> hostname -> 网卡 -> localhost）

---

### 4.9 Server 生命周期 (internal/server/server.go)

#### NewServer 初始化流程

```
1. 初始化存储引擎（NewEngine）
2. 根据 raft.enabled 选择 CP 或 AP 模式
   ├─ CP: NewCPStorage(rawEngine, raftCfg) → RaftNode.Start() → StartServer(raftBindAddr)
   └─ AP: NewAPStorage(rawEngine)
3. 创建 gRPC Server（带 keepalive + interceptor chain）
4. 注册 KVService + HealthService + Reflection
5. 创建 Metrics HTTP Server (:9090)
6. 创建 gRPC Listener
```

#### Start 启动顺序

```
1. 启动 gRPC Server（goroutine）
2. 启动 Metrics HTTP Server（goroutine）
3. 注册到 Etcd
4. 设置 Health Check = SERVING
5. CP 模式：启动 Raft 指标上报 goroutine
```

#### Stop 优雅关闭

```
1. Health Check = NOT_SERVING（停止接收新流量）
2. 停止 Raft 指标 goroutine
3. 撤销 Etcd 注册
4. gRPC GracefulStop（带 timeout fallback）
5. 关闭 Metrics HTTP Server
6. 关闭存储引擎（触发 AOF Close、EventBus Close）
```

---

## 5. 数据流

### 5.1 AP 写入流程

```
Client ──HTTP──▶ Gateway ──gRPC──▶ Client SDK
                                         │
                                         ▼
                                   selectNode(AP)
                                   ├─ consistent hash -> addr
                                   └─ health check fallback
                                         │
                                         ▼
                                   ConnPool.Get(addr)
                                         │
                                         ▼
                                   KV Server (AP Mode)
                                   ├─ APStorage.Set()
                                   │   └─ engine.Set()
                                   │       └─ MemDB.Set()
                                   │           ├─ shard.mu.Lock()
                                   │           ├─ ZeroGCShard.Set()
                                   │           ├─ shard.mu.Unlock()
                                   │           ├─ AOF.AsyncWrite()
                                   │           └─ EventBus.Publish()
                                   │
                                   └─ gRPC response OK
```

### 5.2 CP 写入流程

```
Client ──HTTP──▶ Gateway ──gRPC──▶ Client SDK
                                         │
                                         ▼
                                   selectNode(CP)
                                   ├─ cpGroup.leaderAddr (优先)
                                   └─ healthy follower fallback
                                         │
                                         ▼
                                   KV Server (CP Mode)
                                   ├─ CPStorage.Set()
                                   │   └─ RaftNode.Propose(cmd)
                                   │       ├─ Leader only check
                                   │       ├─ append log locally
                                   │       ├─ go replicateLog()
                                   │       │   └─ send AppendEntries to all peers
                                   │       ├─ wait for majority ACK
                                   │       ├─ update commitIndex
                                   │       └─ applyLoop applies to engine
                                   │
                                   └─ gRPC response OK
```

### 5.3 CP Leader 重试流程

当 Client 向非 Leader CP 节点发送写请求时：

```
1. Client SDK 收到 "not leader" 错误
2. cpRetrySet(): 遍历 CP 组内所有节点
3. 跳过已失败的节点
4. 向下一节点发送 Set 请求
5. 成功即返回，全部失败返回 error
```

### 5.4 CDC 事件流程

```
MemDB.Set()/Del()
    │
    ▼
EventBus.Publish(Event{Type, Key, Value})
    │
    ▼ (100ms timeout fallback)
channel -> Consumer goroutine
    │
    ▼
publishToRabbitMQ()
    │
    ▼
RabbitMQ Exchange (fanout)
    │
    ▼
CDC Consumer (cmd/cdc_consumer)
    │
    ▼
Write to /app/logs/flux_cdc.log
```

---

## 6. 部署架构

### Docker Compose 服务拓扑

```
┌─────────────────────────────────────────────────────────────┐
│                      flux-net (172.25.0.0/16)               │
│                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │  flux-etcd  │  │flux-rabbitmq│  │   flux-jaeger       │ │
│  │   :2379     │  │  :5672      │  │   :4317 / :16686    │ │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘ │
│         │                │                    │            │
│  ┌──────┴──────┐  ┌──────┴──────┐             │            │
│  │             │  │             │             │            │
│  ▼             ▼  ▼             ▼             ▼            │
│  CP-1        CP-2 CP-3        AP-1           AP-2          │
│  :50052      :50053 :50054    :50055         :50056       │
│  :12001      :12002 :12003    :9094          :9095        │
│  :9091       :9092  :9093                                   │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Gateway-1 (:8080)  <──>  Gateway-2 (:8081)          │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
│  ┌─────────────────────┐  ┌─────────────────────────────┐  │
│  │  flux-cdc-consumer  │  │  flux-prometheus (:9090)    │  │
│  │  (reads RabbitMQ)   │  │  flux-prometheus-sd (:8082) │  │
│  └─────────────────────┘  └─────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 服务端口速查

| 服务 | 端口 | 说明 |
|------|------|------|
| CP Node 1-3 | 50052-50054 | gRPC 服务 |
| CP Node 1-3 | 12001-12003 | Raft RPC |
| CP Node 1-3 | 9091-9093 | Metrics HTTP |
| AP Node 1-2 | 50055-50056 | gRPC 服务 |
| AP Node 1-2 | 9094-9095 | Metrics HTTP |
| Gateway 1-2 | 8080-8081 | HTTP API |
| Etcd | 2379 | Client API |
| RabbitMQ | 5672 | AMQP / 15672 Management |
| Jaeger | 4317 | OTLP gRPC / 16686 UI |
| Prometheus | 9090 | Web UI |

---

## 7. 测试结构

### test/ 目录测试覆盖

| 测试文件 | 测试内容 | 数量 |
|----------|----------|------|
| `breaker_test.go` | 熔断器三态转换 | 5 |
| `health_test.go` | 健康探测状态机 | 3 |
| `pool_test.go` | 连接池复用/过期/关闭 | 3 |
| `retry_test.go` | 重试策略（成功/失败/取消） | 7 |
| `raft_election_test.go` | Leader 选举、故障转移、脑裂 | 4 |
| `raft_replication_test.go` | 日志复制、分区安全、提交仲裁 | 6 |

### Raft 测试基础设施

**文件**: `test/raft_mock.go`

```go
type RaftCluster struct {
    nodes map[string]*RaftNodeWrap
}

func (c *RaftCluster) AddNode(nodeID, port, peers)
func (c *RaftCluster) WaitForLeader(timeout)
func (c *RaftCluster) StopNode(id)
func (c *RaftCluster) WaitForApplied(minApplied, timeout)
```

使用真实 gRPC transport 在内存中构建 Raft 集群进行测试。

---

## 8. 关键文件索引

### 按功能快速导航

| 功能 | 关键文件 |
|------|----------|
| **存储引擎接口** | `internal/storage/engine.go` |
| **引擎工厂** | `internal/storage/factory.go` |
| **AP 模式** | `internal/storage/ap_storage.go` |
| **CP 模式** | `internal/storage/cp_storage.go` |
| **MemDB 适配器** | `internal/storage/memdb_adapter.go` |
| **MemDB 核心** | `internal/storage/core/mem_db.go` |
| **ZeroGCShard** | `internal/storage/core/cache.go` |
| **AOF 持久化** | `internal/storage/aof/aof.go` |
| **SimpleMap** | `internal/storage/simplemap.go` |
| **Raft 状态机** | `internal/raft/node.go` |
| **Raft 配置** | `internal/raft/config.go` |
| **Raft 类型** | `internal/raft/types.go` |
| **Raft 应用** | `internal/raft/apply.go` |
| **Raft 传输** | `internal/raft/transport.go` |
| **gRPC 服务实现** | `internal/service/handler.go` |
| **Server 生命周期** | `internal/server/server.go` |
| **网关路由** | `internal/network/gateway/router/router.go` |
| **网关 KV 处理** | `internal/network/gateway/handler/kv.go` |
| **网关 Admin** | `internal/network/gateway/handler/admin.go` |
| **TCP 协议** | `internal/network/protocol/` |
| **客户端 SDK** | `pkg/network/client/client.go` |
| **连接池** | `pkg/network/client/pool.go` |
| **健康检查** | `pkg/network/client/health.go` |
| **熔断器** | `pkg/network/client/breaker.go` |
| **重试** | `pkg/network/client/retry.go` |
| **节点选择** | `pkg/network/client/node_pool.go` |
| **服务注册** | `pkg/network/discovery/register.go` |
| **服务发现** | `pkg/network/discovery/discovery.go` |
| **配置管理** | `internal/config/config.go` |
| **日志** | `pkg/logger/logger.go` |
| **指标** | `pkg/metrics/metrics.go` |
| **一致性哈希** | `pkg/consistent/consistent.go` |
| **限流中间件** | `pkg/middleware/ratelimit.go` |
| **熔断中间件** | `pkg/middleware/circuit_breaker.go` |
| **gRPC Proto** | `api/proto/kv.proto`, `api/proto/raft/raft.proto` |
| **Server 入口** | `cmd/server/main.go` |
| **Gateway 入口** | `cmd/gateway/main.go` |
| **Docker 编排** | `docker-compose.yaml` |

---

## 9. 设计模式汇总

| 模式 | 应用位置 | 说明 |
|------|----------|------|
| **策略模式** | `APStorage` / `CPStorage` | 运行时切换一致性策略 |
| **适配器模式** | `MemDBAdapter` | 将 `core.MemDB` 适配为 `Engine` 接口 |
| **工厂模式** | `NewEngine()` | 根据字符串创建对应引擎 |
| **单例模式** | `pkg/logger.Log` | 全局日志实例 |
| **观察者模式** | `EventBus` | RabbitMQ 事件发布/订阅 |
| **连接池** | `ConnPool` | gRPC 连接复用与生命周期 |
| **熔断器** | `CircuitBreaker` | 故障隔离与自动恢复 |
| **一致性哈希** | `consistent.Map` | AP 节点分布式路由 |
| **请求去重** | `singleflight.Group` | Gateway 并发 GET 合并 |

---

*本文档最后更新于项目审计完成后，反映了最新的代码结构。*
