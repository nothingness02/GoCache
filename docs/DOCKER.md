# Flux-KV Docker 容器化部署指南

## 概览

本文档介绍如何使用 Docker 和 Docker Compose 快速部署 Flux-KV 完整微服务集群。

**支持的服务**:
- **基础设施**：Etcd（服务注册）、RabbitMQ（消息队列）、Jaeger（链路追踪）、Prometheus（监控）
- **CP 存储层**：3 个 Raft 节点（强一致性）
- **AP 存储层**：2 个高可用节点（高性能）
- **网关层**：2 个 gRPC 网关实例（统一路由 + 限流/熔断）
- **数据流层**：CDC Consumer（变更日志）
- **监控层**：Prometheus Service Discovery

---

## 快速开始

### 前置需求

- Docker >= 20.10
- Docker Compose >= 2.0
- 至少 4GB 可用内存（Raft 组需要额外资源）

### 一键启动

```bash
# 进入项目目录
cd Flux-KV

# 创建环境变量文件（如不存在）
cat > .env << 'EOF'
RABBITMQ_USER=fluxadmin
RABBITMQ_PASS=flux2026secure
EOF

# 启动整个集群
./scripts/docker_start.sh

# 或手动启动
docker-compose up --build -d
```

启动后约 20-30 秒所有服务就绪（CP 节点需要完成 Raft 选举）。

### 验证集群

```bash
# 查看所有容器状态
docker-compose ps

# 预期输出：
# NAME                  COMMAND                  SERVICE           STATUS         PORTS
# flux-etcd             "etcd ..."               etcd              Up (healthy)   2379/tcp
# flux-rabbitmq         "rabbitmq-server ..."    rabbitmq          Up (healthy)   5672/tcp, 15672/tcp
# flux-jaeger           "/go/bin/all-in-one..."  jaeger            Up             4317/tcp, 16686/tcp
# flux-cp-node-1        "/app/flux-server ..."   cp-node-1         Up             50052/tcp, 9090/tcp
# flux-cp-node-2        "/app/flux-server ..."   cp-node-2         Up             50053/tcp, 9090/tcp
# flux-cp-node-3        "/app/flux-server ..."   cp-node-3         Up             50054/tcp, 9090/tcp
# flux-ap-node-1        "/app/flux-server ..."   ap-node-1         Up             50055/tcp, 9090/tcp
# flux-ap-node-2        "/app/flux-server ..."   ap-node-2         Up             50056/tcp, 9090/tcp
# flux-gateway-1        "/app/flux-gateway ..."  gateway-1         Up             50051/tcp, 9096/tcp
# flux-gateway-2        "/app/flux-gateway ..."  gateway-2         Up             50051/tcp, 9097/tcp
# flux-cdc-consumer     "/app/flux-consumer..."  cdc-consumer      Up             (no ports)
# flux-prometheus-sd    "/app/flux-promethe..."  prometheus-sd     Up             8080/tcp
# flux-prometheus       "/bin/prometheus ..."    prometheus        Up             9090/tcp
```

---

## 访问服务

### gRPC API（Gateway）

Gateway 对外暴露 **gRPC 接口**（端口 50051/50052），外部程序通过 `pkg/network/client` 中的 gRPC Client SDK 连接。

> **注意**: Raft RPC 与业务 gRPC 共享端口。`FLUX_RAFT_PEERS` 应指向业务端口（如 `cp-node-2:50052`），不再需要独立的 Raft 端口。

Gateway 同时暴露 HTTP 管理接口（端口 9096/9097）用于 Prometheus 指标和 admin 查询。

```bash
# === 使用内置交互式客户端 ===
go run ./cmd/client/main.go

# 在提示符下输入：
# SET docker-test container-success ap
# GET docker-test ap
# DEL docker-test ap

# === 使用 benchmark 压测 ===
go run ./cmd/benchmark/main.go -c 10 -n 1000 -mode ap
go run ./cmd/benchmark/main.go -c 10 -n 1000 -mode cp

# === 在自定义 Go 程序中使用 Client SDK ===
# import "Flux-KV/pkg/network/client"
# conn, _ := client.NewDirectConn("localhost:50051")
# cli := client.NewClient(conn)
# cli.SetWithMode(ctx, "key", "value", "ap")
```

### Admin / Metrics 接口（HTTP）

```bash
# 限流/熔断状态
curl http://localhost:9096/admin/resilience

# 节点列表
curl http://localhost:9096/admin/nodes

# 集群统计
curl http://localhost:9096/admin/stats

# 指定节点状态
curl "http://localhost:9096/admin/nodes/cp-node-1:50052/status"

# Prometheus 指标
curl http://localhost:9096/metrics | grep circuit_breaker
curl http://localhost:9096/metrics | grep rate_limited
curl http://localhost:9096/metrics | grep grpc_requests
```

### Jaeger 链路追踪

访问 **http://localhost:16686**，选择 Service: `kv-service`，查看完整的分布式追踪。

### RabbitMQ 管理界面

访问 **http://localhost:15672**（用户/密码见 `.env` 文件中的 `RABBITMQ_USER` / `RABBITMQ_PASS`）

### Prometheus 监控

访问 **http://localhost:9090**，可查询以下指标：

```promql
# Raft 角色分布 (0=Follower, 1=Candidate, 2=Leader)
raft_role

# Raft 提交索引
raft_commit_index

# 网关后端节点数
gateway_backend_nodes

# 网关请求总量
gateway_requests_total

# KV 请求总量
kv_requests_total
```

### Etcd CLI

```bash
# 查看所有注册的 KV Server（包含 NodeInfo JSON）
docker exec flux-etcd etcdctl get /services/kv-service --prefix

# 预期输出包含 mode、group_id、is_leader 等元数据
```

---

## 环境变量配置

所有配置通过 `FLUX_` 前缀的环境变量控制，优先级：**环境变量 > config.yaml > 默认值**

### 通用配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FLUX_SERVER_PORT` | `50052` | gRPC 服务端口 |
| `FLUX_ETCD_ENDPOINTS` | `etcd:2379` | Etcd 地址（逗号分隔） |
| `FLUX_RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | RabbitMQ 连接串 |
| `FLUX_JAEGER_ENDPOINT` | `jaeger:4317` | Jaeger OTLP 端点 |
| `FLUX_AOF_FILENAME` | `/app/data/go-kv.aof` | AOF 文件路径 |
| `FLUX_PPROF_ENABLED` | `false` | 是否启用性能分析 |
| `FLUX_PPROF_PORT` | `6060` | Pprof 监听端口 |
| `FLUX_POD_IP` | - | 节点 IP/主机名（用于 Etcd 注册） |

### Gateway 配置（仅 Gateway 节点）

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FLUX_GATEWAY_GRPC_PORT` | `50051` | Gateway gRPC 服务端口 |
| `FLUX_GATEWAY_METRICS_PORT` | `19090` | Gateway HTTP 管理/指标端口 |

### 存储引擎配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FLUX_STORAGE_ENGINE` | `memdb` | 存储引擎: `memdb` (分片锁) / `simplemap` (标准 map) |

### Raft 配置（仅 CP 节点）

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FLUX_RAFT_ENABLED` | `false` | 是否启用 Raft（true=CP 模式, false=AP 模式） |
| `FLUX_RAFT_NODE_ID` | `node-1` | Raft 节点唯一标识 |
| `FLUX_RAFT_GROUP_ID` | `cp-group-1` | Raft 节点组标识 |
| `FLUX_RAFT_PEERS` | - | 同组其他节点地址列表，逗号分隔（如 `cp-node-2:12002,cp-node-3:12003`） |
| `FLUX_RAFT_BIND_ADDR` | `:12000` | Raft 节点间通信绑定地址 |
| `FLUX_RAFT_DATA_DIR` | `/app/data/raft` | Raft 日志和数据目录 |

### CDC 配置（仅 Consumer）

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FLUX_CDC_EXCHANGE` | `flux_kv_events` | RabbitMQ CDC Exchange |
| `FLUX_CDC_QUEUE` | `flux_cdc_file_logger` | CDC 消费队列名 |
| `FLUX_CDC_LOG_PATH` | `/app/logs/flux_cdc.log` | CDC 日志文件路径 |

### 例子：修改配置

编辑 `.env` 文件：
```bash
# 修改 RabbitMQ 认证信息
RABBITMQ_USER=admin
RABBITMQ_PASS=MySecurePassword123!
```

修改后重启容器：
```bash
docker-compose restart
```

---

## 容器架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Docker Network (flux-net)                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────┐               │
│  │   Etcd      │  │  RabbitMQ    │  │   Jaeger    │               │
│  │  :2379      │  │   :5672      │  │  :16686     │               │
│  └─────────────┘  └──────────────┘  └─────────────┘               │
│        ↑                 ↓                                          │
│        │            ┌─────────────┐                                │
│        │            │  Consumer   │                                │
│        │            │  flux_cdc.log                                │
│        │            └─────────────┘                                │
│        │                                                            │
│  ┌──────────────────────────────────────┐                         │
│  │         CP Storage Layer             │                         │
│  │    (Raft Strong Consistency)         │                         │
│  ├──────────────────────────────────────┤                         │
│  │  [cp-node-1] [cp-node-2] [cp-node-3] │                         │
│  │   :50052      :50053      :50054     │                         │
│  │   :12001      :12002      :12003     │ ← Raft Port             │
│  │   Leader?     Follower    Follower   │                         │
│  └──────────────────────────────────────┘                         │
│        ↑              ↑              ↑                              │
│        └──────────────┼──────────────┘                              │
│                       │                                             │
│  ┌──────────────────────────────────────┐                         │
│  │         AP Storage Layer             │                         │
│  │    (High Availability)               │                         │
│  ├──────────────────────────────────────┤                         │
│  │  [ap-node-1]        [ap-node-2]      │                         │
│  │   :50055             :50056          │                         │
│  │   simplemap          simplemap       │                         │
│  └──────────────────────────────────────┘                         │
│        ↑              ↑                                             │
│        └──────────────┼──────────────┘                              │
│                       │                                             │
│              ┌──────────────────────┐                              │
│              │     Gateway x2       │                              │
│              │  gRPC :50051         │                              │
│              │  Admin:19090→9096/7  │                              │
│              └──────────────────────┘                              │
│                       │                                             │
│   gRPC: localhost:50051/50052  Admin: localhost:9096/9097          │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 常见操作

### 查看容器日志

```bash
# 所有服务
docker-compose logs -f

# 特定服务
docker-compose logs -f cp-node-1
docker-compose logs -f ap-node-1
docker-compose logs -f gateway-1
docker-compose logs -f cdc-consumer

# 最后 100 行日志
docker-compose logs --tail=100
```

### 重启单个服务

```bash
# 重启 CP Leader（观察 Raft 选举过程）
docker-compose restart cp-node-1

# 重启 AP 节点
docker-compose restart ap-node-1

# 重启 Gateway
docker-compose restart gateway-1
```

### 进入容器执行命令

```bash
# 进入 CP 节点 Shell
docker exec -it flux-cp-node-1 sh

# 查看 AOF 文件
docker exec flux-cp-node-1 cat /app/data/cp-node-1.aof

# 查看 Raft 数据目录
docker exec flux-cp-node-1 ls -la /app/data/raft/

# 检查 Etcd 注册信息
docker exec flux-etcd etcdctl get /services/kv-service --prefix
```

### 停止和清理

```bash
# 优雅停止（保留数据）
./scripts/docker_stop.sh

# 或使用 docker-compose 直接停止
docker-compose stop -t 30

# 删除容器但保留 Volume（数据不丢失）
docker-compose down

# 删除一切包括 Volume（危险！）
./scripts/docker_clean.sh  # 或 docker-compose down -v
```

---

## 功能验证清单

### 1. 基础连通性

```bash
✓ Gateway gRPC 端口连通性
nc -z localhost 50051 && echo "gRPC port open"

✓ Gateway Admin 接口
curl http://localhost:9096/admin/resilience
# {"rate_limiter":{"enabled":true,...},"circuit_breaker":{"enabled":true,...}}

✓ AP 模式写入/读取（使用交互式客户端）
go run ./cmd/client/main.go
# 在提示符下输入：
# SET docker-test value1 ap
# GET docker-test ap
# DEL docker-test ap

✓ CP 模式写入/读取
go run ./cmd/client/main.go
# 在提示符下输入：
# SET docker-test value2 cp
# GET docker-test cp
# DEL docker-test cp
```

### 2. 管理接口

```bash
✓ 集群拓扑
curl http://localhost:9096/admin/nodes

✓ 节点状态（CP）
curl "http://localhost:9096/admin/nodes/cp-node-1:50052/status"
# 应包含 raft_info.role、raft_info.commit_index 等

✓ 节点状态（AP）
curl "http://localhost:9096/admin/nodes/ap-node-1:50052/status"
# 应包含 stats.entry_count、stats.memory_bytes，raft_info 为 null

✓ 集群统计
curl http://localhost:9096/admin/stats
```

### 3. Raft 故障转移验证

```bash
# 步骤 1: 确认当前 Leader
curl "http://localhost:9096/admin/nodes/cp-node-1:50052/status" | grep role
curl "http://localhost:9096/admin/nodes/cp-node-2:50052/status" | grep role
curl "http://localhost:9096/admin/nodes/cp-node-3:50052/status" | grep role

# 步骤 2: 停止当前 Leader
docker-compose stop cp-node-1

# 步骤 3: 等待约 5-10 秒，观察剩余节点选举新 Leader
curl "http://localhost:9096/admin/nodes/cp-node-2:50052/status" | grep role
curl "http://localhost:9096/admin/nodes/cp-node-3:50052/status" | grep role

# 步骤 4: CP 写入仍然可用（客户端自动重定向到新 Leader）
go run ./cmd/client/main.go
# SET failover-test after-leader-change cp

# 步骤 5: 恢复旧 Leader
docker-compose start cp-node-1
```

### 4. 数据持久化

```bash
# 写入数据（AP 模式）
go run ./cmd/client/main.go
# SET persist-test before-restart ap

# 重启 AP 节点
docker-compose restart ap-node-1
sleep 10

# 数据应该仍然存在（从 AOF 恢复）
go run ./cmd/client/main.go
# GET persist-test ap
```

### 5. CDC 事件流

```bash
# 查看 CDC 消费者日志
docker exec flux-cdc-consumer cat /app/logs/flux_cdc.log | tail -20

# 应该看到 SET/DEL 事件的 JSON 格式
```

### 6. 链路追踪

```bash
# 生成追踪数据（使用 benchmark 快速产生请求）
go run ./cmd/benchmark/main.go -c 5 -n 30 -mode ap

# 打开 http://localhost:16686
# 选择 Service: kv-service
# 应该看到多条 Trace，每条显示 Gateway -> Server 的调用链
```

### 7. Prometheus 指标验证

```bash
# 查看 CP 节点 Raft 指标
curl -s http://localhost:9091/metrics | grep "^raft_"

# 查看 AP 节点指标
curl -s http://localhost:9094/metrics | grep "^kv_"

# 查看 Gateway 指标（含限流/熔断）
curl -s http://localhost:9096/metrics | grep -E "^gateway_|^grpc_|^circuit_breaker_|^rate_limited_"
```

---

## 安全建议

### 生产环境配置

1. **修改 RabbitMQ 密码**
   ```bash
   # 编辑 .env
   RABBITMQ_PASS=YourStrongPassword123!@#
   ```

2. **禁用 Pprof**（生产环境应该关闭）
   ```yaml
   # docker-compose.yaml 中修改
   - FLUX_PPROF_ENABLED=false
   ```

3. **限制网络暴露**
   ```bash
   # 只允许内网访问 Pprof（通过防火墙或网络策略）
   # 生产环境不应暴露 6060、9091-9095、12001-12003 端口
   ```

4. **启用 Etcd 认证**（高级配置）
   ```yaml
   # bitnami/etcd 镜像支持认证
   environment:
     - ETCD_ROOT_PASSWORD=your_secure_password
   ```

5. **资源限制**
   ```yaml
   # 为每个服务添加资源限制
   services:
     cp-node-1:
       deploy:
         resources:
           limits:
             cpus: '1.0'
             memory: 1G
           reservations:
             cpus: '0.5'
             memory: 512M
   ```

---

## 故障排查

### 容器无法启动

```bash
# 查看详细错误日志
docker-compose logs <service-name>

# 例子：CP 节点启动失败
docker-compose logs cp-node-1

# 检查依赖服务是否就绪
docker-compose ps
```

### Raft 选举失败

```bash
# 检查 CP 节点间网络连通性
docker exec flux-cp-node-1 ping cp-node-2
docker exec flux-cp-node-1 ping cp-node-3

# 检查 Raft 端口是否开放
docker exec flux-cp-node-1 netstat -tlnp | grep 12001

# 查看 CP 节点日志中的 Raft 状态
docker-compose logs cp-node-1 | grep -i raft
```

### 服务间无法通信

```bash
# 检查网络连接
docker exec flux-gateway-1 ping cp-node-1
docker exec flux-gateway-1 ping ap-node-1

# 应该得到正常的 PING 响应（说明 DNS 解析正常）
```

### Etcd 注册失败

```bash
# 检查 Etcd 是否运行
docker exec flux-etcd etcdctl endpoint health

# 检查 Server 是否成功注册（应包含 NodeInfo JSON）
docker exec flux-etcd etcdctl get /services/kv-service --prefix

# 如果没有注册信息，检查 Server 日志
docker-compose logs cp-node-1 | grep -i etcd
docker-compose logs ap-node-1 | grep -i etcd
```

### RabbitMQ 连接异常

```bash
# 检查 RabbitMQ 状态
docker exec flux-rabbitmq rabbitmq-diagnostics status

# 检查用户是否存在
docker exec flux-rabbitmq rabbitmqctl list_users

# 重置密码（如果需要）
docker exec flux-rabbitmq rabbitmqctl change_password fluxadmin newpassword
```

### CP 模式写入失败

```bash
# 检查是否有 Leader
curl http://localhost:9096/admin/stats | grep leaders

# 如果 leaders 为空，说明 Raft 组未选举出 Leader
# 检查 CP 节点日志
docker-compose logs cp-node-1 | grep -E "Leader|Candidate|election"
```

---

## 性能监控

### 使用 Pprof 分析性能

启用 Pprof（仅用于开发/测试）：

```bash
# CP 节点已启用 Pprof（端口 9091-9093 映射了内部的 9090）
# AP 节点已启用 Pprof（端口 9094-9095 映射了内部的 9090）

# 访问性能分析
curl http://localhost:9091/debug/pprof

# 生成 CPU 火焰图（以 cp-node-1 为例）
go tool pprof -http=:8001 http://localhost:9091/debug/pprof/profile?seconds=30
```

### 查看内存使用

```bash
# Docker 自带的内存监控
docker stats flux-cp-node-1 flux-ap-node-1 flux-gateway-1 flux-rabbitmq

# 实时更新（按 Ctrl+C 退出）
```

---

## 升级和扩展

### 添加第 4 个 CP 节点

```yaml
# 编辑 docker-compose.yaml，复制 cp-node-3 并修改：
cp-node-4:
  build:
    context: .
    dockerfile: Dockerfile.server
  container_name: flux-cp-node-4
  hostname: cp-node-4
  environment:
    - FLUX_ETCD_ENDPOINTS=etcd:2379
    - FLUX_RABBITMQ_URL=amqp://${RABBITMQ_USER}:${RABBITMQ_PASS}@rabbitmq:5672/
    - FLUX_JAEGER_ENDPOINT=jaeger:4317
    - FLUX_AOF_FILENAME=/app/data/cp-node-4.aof
    - FLUX_PPROF_ENABLED=true
    - FLUX_POD_IP=cp-node-4
    - FLUX_STORAGE_ENGINE=memdb
    - FLUX_RAFT_ENABLED=true
    - FLUX_RAFT_NODE_ID=cp-4
    - FLUX_RAFT_GROUP_ID=cp-group-1
    - FLUX_RAFT_PEERS=cp-node-1:12001,cp-node-2:12002,cp-node-3:12003
    - FLUX_RAFT_BIND_ADDR=:12004
    - FLUX_RAFT_DATA_DIR=/app/data/raft
  ports:
    - "50057:50052"
    - "12004:12004"
    - "9098:9090"
  volumes:
    - cp-node-4-data:/app/data
  networks:
    - flux-net
  depends_on:
    etcd:
      condition: service_healthy
    rabbitmq:
      condition: service_healthy
    jaeger:
      condition: service_started
  restart: unless-stopped

# 然后添加对应的 Volume
volumes:
  cp-node-4-data:
```

**重要**: 添加新节点后，需要在现有 CP 节点的 `FLUX_RAFT_PEERS` 中加入 `cp-node-4:12004`，然后重启现有节点。

### 添加第 3 个 AP 节点

```yaml
# 编辑 docker-compose.yaml，复制 ap-node-2 并修改：
ap-node-3:
  build:
    context: .
    dockerfile: Dockerfile.server
  container_name: flux-ap-node-3
  hostname: ap-node-3
  environment:
    - FLUX_ETCD_ENDPOINTS=etcd:2379
    - FLUX_RABBITMQ_URL=amqp://${RABBITMQ_USER}:${RABBITMQ_PASS}@rabbitmq:5672/
    - FLUX_JAEGER_ENDPOINT=jaeger:4317
    - FLUX_AOF_FILENAME=/app/data/ap-node-3.aof
    - FLUX_PPROF_ENABLED=true
    - FLUX_POD_IP=ap-node-3
    - FLUX_STORAGE_ENGINE=simplemap
    - FLUX_RAFT_ENABLED=false
  ports:
    - "50058:50052"
    - "9099:9090"
  volumes:
    - ap-node-3-data:/app/data
  networks:
    - flux-net
  depends_on:
    etcd:
      condition: service_healthy
    rabbitmq:
      condition: service_healthy
    jaeger:
      condition: service_started
  restart: unless-stopped

# 然后添加对应的 Volume
volumes:
  ap-node-3-data:
```

### 添加 Gateway 实例

```yaml
# 复制 gateway-2 并修改端口映射
gateway-3:
  build:
    context: .
    dockerfile: Dockerfile.gateway
  container_name: flux-gateway-3
  hostname: gateway-3
  environment:
    - FLUX_ETCD_ENDPOINTS=etcd:2379
    - FLUX_JAEGER_ENDPOINT=jaeger:4317
    - FLUX_GATEWAY_GRPC_PORT=50051
    - FLUX_GATEWAY_METRICS_PORT=19090
    - FLUX_PPROF_ENABLED=false
    - FLUX_PPROF_PORT=6060
  ports:
    - "50053:50051"   # gRPC 端口（宿主机 50053 -> 容器 50051）
    - "9098:19090"    # Admin/Metrics 端口
  networks:
    - flux-net
  depends_on:
    etcd:
      condition: service_healthy
    cp-node-1:
      condition: service_started
    ap-node-1:
      condition: service_started
  restart: unless-stopped
```

---

## 数据备份和恢复

### 备份 AOF 和 Raft 数据

```bash
# 备份所有 CP 节点 Volume
for i in 1 2 3; do
  docker run --rm -v go_cache_cp-node-${i}-data:/data \
    -v $(pwd)/backups:/backup alpine \
    tar czf /backup/cp-node-${i}-$(date +%Y%m%d-%H%M%S).tar.gz -C /data .
done

# 备份所有 AP 节点 Volume
for i in 1 2; do
  docker run --rm -v go_cache_ap-node-${i}-data:/data \
    -v $(pwd)/backups:/backup alpine \
    tar czf /backup/ap-node-${i}-$(date +%Y%m%d-%H%M%S).tar.gz -C /data .
done
```

### 恢复 AOF 和 Raft 数据

```bash
# 停止容器
docker-compose stop

# 清空 Volume（谨慎！）
docker volume rm go_cache_cp-node-1-data

# 恢复数据
docker run --rm -v go_cache_cp-node-1-data:/data \
  -v $(pwd)/backups:/backup alpine \
  tar xzf /backup/cp-node-1-backup.tar.gz -C /data

# 重启容器
docker-compose start
```

---

## 常见问题（FAQ）

**Q: AP 和 CP 模式可以混合使用同一个 Key 吗？**

A: 不建议。AP 和 CP 节点组是物理隔离的，同一个 Key 在 AP 和 CP 组中会被存储到不同的节点，互不可见。建议根据业务场景选择固定模式。

**Q: CP 模式的性能为什么比 AP 模式低？**

A: CP 模式需要通过 Raft 共识：写入请求必须发送到 Leader，Leader 将日志复制到多数派（Majority）节点后才能提交。这个共识过程引入了网络往返延迟。

**Q: 如何修改 Gateway 的 gRPC 端口为 8888？**

A: 编辑 docker-compose.yaml，修改 Gateway 的 `ports` 和 `environment` 部分：
```yaml
environment:
  - FLUX_GATEWAY_GRPC_PORT=8888
ports:
  - "8888:8888"  # 宿主机 8888 → 容器 8888
```

**Q: 能否在生产环境直接使用这个 Compose 文件？**

A: 需要以下调整：
1. 修改所有密码
2. 禁用 Pprof
3. 添加资源限制
4. 不暴露 Raft 端口到宿主机（仅在 Docker 网络内通信）
5. 改用 Kubernetes 而不是 Docker Compose

**Q: CP 节点停止后数据会丢失吗？**

A: 不会。CP 节点启用了 AOF 持久化和 Raft 日志持久化（存储在 Volume 中）。只要多数派节点存活，集群就能继续服务。停止的节点重启后会从 AOF 和 Raft 日志恢复状态。

**Q: 支持跨主机部署吗？**

A: Docker Compose 本身仅支持单机。需要改用 Swarm 或 Kubernetes。跨主机部署时需要将 `FLUX_RAFT_PEERS` 中的主机名改为实际 IP 地址。

---

## 相关文档

- [API 接口文档](API.md)
- [架构设计文档](../README.md)
- [性能测试报告](../PERFORMANCE.md)

---

### 运维脚本

```bash
# 查看集群状态
./scripts/status.sh

# 查看各节点日志
./scripts/logs.sh <node-name>

# 运行集群集成测试
./scripts/test_cluster.sh

# 验证优雅关闭
./scripts/verify_shutdown.sh
```

---

**最后更新**: 2026-05-21
