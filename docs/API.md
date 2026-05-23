# API Reference

## Base URL

`http://localhost:8080/api/v1`

## Authentication

目前接口为公开访问（Public），但在网关层集成了 **Token Bucket 限流** 机制保护。

---

## Consistency Mode (一致性模式)

所有 KV 操作支持通过 `mode` 查询参数选择一致性模式：

| 模式 | 参数值 | 说明 | 适用场景 |
|------|--------|------|----------|
| **AP** (默认) | `?mode=ap` 或省略 | 高可用，直接写本地存储 | 缓存、会话、高频读写 |
| **CP** | `?mode=cp` | 强一致性，通过 Raft 共识提交 | 配置、元数据、事务性数据 |

> CP 模式的写入请求会自动路由到 CP 组的 Leader 节点。如果 Leader 发生变更，客户端会自动重试其他节点并更新 Leader 缓存。

---

## Key-Value Operations

### 1. Set Value (写入/更新)

将键值对存储到分布式集群中。此操作是**强一致性**写入内存，并**异步**触发 CDC 事件。

- **URL**: `/kv`
- **Method**: `POST`
- **Content-Type**: `application/json`

| Parameter | Type   | Required | Description |
| :---      | :---   | :---     | :---        |
| `key`     | string | Yes      | 键名 (e.g. `user:1001`) |
| `value`   | string | Yes      | 键值 (e.g. `{"name": "wang"}`) |
| `mode`    | string | No       | 一致性模式: `ap` (默认) / `cp` |

**Example:**
```bash
# AP 模式 (默认)
curl -X POST http://localhost:8080/api/v1/kv \
  -H "Content-Type: application/json" \
  -d '{"key":"name","value":"naato"}'

# CP 模式
curl -X POST "http://localhost:8080/api/v1/kv?mode=cp" \
  -H "Content-Type: application/json" \
  -d '{"key":"config","value":"value"}'
```

**Success Response:**
```json
{
    "message": "success",
    "key": "name",
    "value": "naato",
    "mode": "ap"
}
```

**Error Response:**
- `400 Bad Request`: 参数错误（缺少 key/value 或 JSON 格式错误）
- `429 Too Many Requests`: 触发全局限流
- `502 Bad Gateway`: Gateway 无法连接到后端节点
- `504 Gateway Timeout`: 后端节点响应超时
- `503 Service Unavailable`: 触发熔断降级
- `500 Internal Server Error`: 存储失败（CP 模式下可能是无 Leader 可用）

---

### 2. Get Value (读取)

根据 Key 获取 Value。

- **URL**: `/kv`
- **Method**: `GET`
- **Query Params**:
    - `key`: 目标键名 (Required)
    - `mode`: 一致性模式 `ap` (默认) / `cp` (Optional)

**Example**:
```bash
# AP 模式 (默认)
curl "http://localhost:8080/api/v1/kv?key=user:1001"

# CP 模式
curl "http://localhost:8080/api/v1/kv?key=config&mode=cp"
```

**Response:**
```json
{
    "key": "user:1001",
    "value": "{\"name\": \"wang\"}",
    "shared": false,
    "mode": "ap"
}
```

- `shared`: 表示是否命中 SingleFlight 合并请求（防缓存击穿）

**Error Response:**
- `400 Bad Request`: 缺少 key 参数
- `404 Not Found`: Key 不存在或查询失败

---

### 3. Delete Value (删除)

删除指定的 Key。

- **URL**: `/kv`
- **Method**: `DELETE`
- **Query Params**:
    - `key`: 目标键名 (Required)
    - `mode`: 一致性模式 `ap` (默认) / `cp` (Optional)

**Example**:
```bash
# AP 模式
curl -X DELETE "http://localhost:8080/api/v1/kv?key=user:1001"

# CP 模式
curl -X DELETE "http://localhost:8080/api/v1/kv?key=config&mode=cp"
```

**Response:**
```json
{
    "message": "deleted",
    "key": "user:1001",
    "mode": "ap"
}
```

---

## Admin Interfaces (管理接口)

管理接口用于查看集群拓扑、节点状态和统计信息，**不经过熔断器/限流器**。

### 1. List Nodes (集群拓扑)

列出 Etcd 中注册的所有 KV 服务节点及其元数据。

- **URL**: `/admin/nodes`
- **Method**: `GET`

**Example**:
```bash
curl http://localhost:8080/admin/nodes
```

**Response:**
```json
[
    {
        "key": "/services/kv-service/cp-node-1:50052",
        "node_id": "cp-1",
        "addr": "cp-node-1:50052",
        "group_id": "cp-group-1",
        "mode": "cp",
        "is_leader": true,
        "engine_type": "memdb",
        "raft_addr": ":12001"
    },
    {
        "key": "/services/kv-service/ap-node-1:50052",
        "node_id": "",
        "addr": "ap-node-1:50052",
        "group_id": "ap-group-1",
        "mode": "ap",
        "is_leader": false,
        "engine_type": "simplemap",
        "raft_addr": ""
    }
]
```

---

### 2. Node Status (节点状态)

通过 gRPC 调用指定节点的 `Status` 接口，获取引擎统计和 Raft 状态。

- **URL**: `/admin/nodes/:addr/status`
- **Method**: `GET`
- **Path Params**:
    - `addr`: 节点地址，需要进行 URL 编码 (e.g. `cp-node-1%3A50052`)

**Example**:
```bash
# 查询 CP 节点状态
curl "http://localhost:8080/admin/nodes/cp-node-1:50052/status"

# 查询 AP 节点状态
curl "http://localhost:8080/admin/nodes/ap-node-1:50052/status"
```

**CP Node Response:**
```json
{
    "node_id": "cp-1",
    "mode": "cp",
    "engine_type": "memdb",
    "stats": {
        "engine_type": "memdb",
        "entry_count": 150,
        "memory_bytes": 24576
    },
    "healthy": true,
    "raft": {
        "role": "Leader",
        "term": 5,
        "commit_index": 1280,
        "last_applied": 1280,
        "healthy_nodes": 3,
        "peers": ["cp-node-2:12002", "cp-node-3:12003"]
    }
}
```

**AP Node Response:**
```json
{
    "node_id": "",
    "mode": "ap",
    "engine_type": "simplemap",
    "stats": {
        "engine_type": "simplemap",
        "entry_count": 3200,
        "memory_bytes": 524288
    },
    "healthy": true,
    "raft": null
}
```

---

### 3. Cluster Stats (集群统计)

汇总所有节点的状态信息。

- **URL**: `/admin/stats`
- **Method**: `GET`

**Example**:
```bash
curl http://localhost:8080/admin/stats
```

**Response:**
```json
{
    "total_nodes": 5,
    "cp_nodes": 3,
    "ap_nodes": 2,
    "leaders": ["cp-node-1:50052"],
    "nodes": [
        { /* ... 各节点状态 ... */ }
    ]
}
```

---

## System Check

### Health Probe

用于 K8s 或 Docker Compose 的健康检查探针。

- **URL**: `/health`
- **Method**: `GET`

**Response:**
```json
{
    "status": "ok",
    "time": "2026-02-12T10:00:00Z"
}
```

---

## gRPC Service Methods

除 HTTP 网关外，KV Server 直接暴露以下 gRPC 方法（定义于 `api/proto/kv.proto`）：

### KVService

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `Set` | `SetRequest` {key, value} | `SetResponse` | 写入键值 |
| `Get` | `GetRequest` {key} | `GetResponse` {value} | 读取键值 |
| `Del` | `DelRequest` {key} | `DelResponse` | 删除键值 |
| `Status` | `StatusRequest` | `StatusResponse` | 节点状态（引擎统计、健康状态） |
| `RaftInfo` | `RaftInfoRequest` | `RaftInfoResponse` | Raft 状态（角色、任期、提交索引） |

> `Status` 和 `RaftInfo` 主要用于管理接口和监控采集。AP 节点调用 `RaftInfo` 会返回错误。

---

## Error Codes

| HTTP Status | gRPC Code | 说明 |
|-------------|-----------|------|
| 400 | `INVALID_ARGUMENT` | 参数错误 |
| 404 | `NOT_FOUND` | Key 不存在 |
| 429 | `RESOURCE_EXHAUSTED` | 触发限流 |
| 500 | `INTERNAL` | 内部错误（存储失败、Raft 无 Leader 等） |
| 503 | `UNAVAILABLE` | 服务不可用（熔断触发） |

---

**最后更新**: 2026-05-20

---

## Admin Endpoints

### 1. Health Check (存活探测)

```
GET /health
```

返回简单存活状态，进程运行即返回 200。

**Response:**
```json
{
  "status": "ok",
  "time": "2026-05-23T10:00:00Z",
  "node": "node-1",
  "mode": "ap"
}
```

### 2. Readiness Probe (就绪探测)

```
GET /ready
```

检查服务是否真正就绪（Etcd 连通、存储初始化完成）。

**Ready Response (200):**
```json
{
  "status": "ready",
  "time": "2026-05-23T10:00:00Z",
  "node": "node-1",
  "mode": "ap"
}
```

**Not Ready Response (503):**
```json
{
  "status": "not_ready",
  "checks": ["etcd: connection refused"],
  "time": "2026-05-23T10:00:00Z"
}
```

### 3. Metrics (Prometheus)

```
GET /metrics
```

暴露 Prometheus 格式指标，包括：
- `flux_kv_grpc_requests_total` — gRPC 请求总数
- `flux_kv_grpc_request_duration_seconds` — 请求延迟分布
- `flux_kv_aof_commands_total` — AOF 写入命令数
- `flux_raft_role` — Raft 节点角色（0=Follower, 1=Candidate, 2=Leader）
