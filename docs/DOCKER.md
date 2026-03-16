# Flux-KV Docker 容器化部署指南

## 📋 概览

本文档介绍如何使用 Docker 和 Docker Compose 快速部署 Flux-KV 完整微服务集群。

**支持的服务**:
- **基础设施**：Etcd（服务注册）、RabbitMQ（消息队列）、Jaeger（链路追踪）
- **存储层**：3 个 KV Server 实例（集群）
- **网关层**：HTTP API 网关（负载均衡）
- **数据流层**：CDC Consumer（变更日志）

---

## 🚀 快速开始

### 前置需求

- Docker >= 20.10
- Docker Compose >= 2.0
- 至少 2GB 可用内存

### 一键启动

```bash
# 进入项目目录
cd Flux-KV

# 启动整个集群
./scripts/docker_start.sh

# 或手动启动
docker-compose up -d
```

启动后约 15 秒所有服务就绪。

### 验证集群

```bash
# 查看所有容器状态
docker-compose ps

# 预期输出：
# NAME                COMMAND                  SERVICE      STATUS         PORTS
# flux-etcd           "etcd ..."              etcd         Up (healthy)   2379/tcp
# flux-rabbitmq       "rabbitmq-server ..."   rabbitmq     Up (healthy)   5672/tcp, 15672/tcp
# flux-jaeger         "/go/bin/all-in-one..." jaeger       Up             4317/tcp, 16686/tcp
# flux-kv-server-1    "/app/flux-server ..."  kv-server-1  Up             50052/tcp
# flux-kv-server-2    "/app/flux-server ..."  kv-server-2  Up             50053/tcp
# flux-kv-server-3    "/app/flux-server ..."  kv-server-3  Up             50054/tcp
# flux-gateway        "/app/flux-gateway ..." gateway      Up             8080/tcp, 6060/tcp
# flux-cdc-consumer   "/app/flux-consumer..." cdc-consumer Up             (no ports)
```

---

## 📡 访问服务

### HTTP API（Gateway）

```bash
# 健康检查
curl http://localhost:8080/health

# 写入数据
curl -X POST http://localhost:8080/api/v1/kv \
  -H "Content-Type: application/json" \
  -d '{"key":"docker-test","value":"container-success"}'

# 读取数据
curl http://localhost:8080/api/v1/kv?key=docker-test

# 删除数据
curl -X DELETE http://localhost:8080/api/v1/kv?key=docker-test
```

### Jaeger 链路追踪

访问 **http://localhost:16686**，选择 Service: `kv-service`，查看完整的分布式追踪。

### RabbitMQ 管理界面

访问 **http://localhost:15672**（用户：`fluxadmin`，密码：`flux2026secure`）

### Etcd CLI

```bash
# 查看所有注册的 KV Server
docker exec flux-etcd etcdctl get /services/kv-service --prefix

# 预期输出：
# /services/kv-service/kv-server-1:50052
# kv-server-1:50052
# /services/kv-service/kv-server-2:50052
# kv-server-2:50052
# ...
```

---

## 🛠️ 环境变量配置

所有配置通过 `FLUX_` 前缀的环境变量控制，优先级：**环境变量 > config.yaml > 默认值**

### 可配置项清单

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `FLUX_SERVER_PORT` | `50052` | gRPC 服务端口 |
| `FLUX_GATEWAY_PORT` | `8080` | HTTP 网关端口 |
| `FLUX_ETCD_ENDPOINTS` | `etcd:2379` | Etcd 地址（逗号分隔） |
| `FLUX_RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | RabbitMQ 连接串 |
| `FLUX_JAEGER_ENDPOINT` | `jaeger:4317` | Jaeger OTLP 端点 |
| `FLUX_AOF_FILENAME` | `/app/data/go-kv.aof` | AOF 文件路径 |
| `FLUX_PPROF_ENABLED` | `false` | 是否启用性能分析 |
| `FLUX_PPROF_PORT` | `6060` | Pprof 监听端口 |
| `FLUX_CDC_EXCHANGE` | `flux_kv_events` | RabbitMQ CDC Exchange |
| `FLUX_CDC_QUEUE` | `flux_cdc_file_logger` | CDC 消费队列名 |
| `FLUX_CDC_LOG_PATH` | `/app/logs/flux_cdc.log` | CDC 日志文件路径 |

### 例子：修改配置

编辑 `.env` 文件：
```bash
# 修改 RabbitMQ 认证信息
RABBITMQ_USER=admin
RABBITMQ_PASS=MySecurePassword123!

# 为 Compose 文件中的环境变量所用
```

修改后重启容器：
```bash
docker-compose restart
```

---

## 📊 容器架构

```
┌─────────────────────────────────────────────────┐
│      Docker Network (flux-net)                  │
├─────────────────────────────────────────────────┤
│                                                 │
│  ┌─────────────┐  ┌──────────────┐             │
│  │   Etcd      │  │  RabbitMQ    │ ← Jaeger   │
│  │  :2379      │  │   :5672      │            │
│  └─────────────┘  └──────────────┘            │
│        ↑                 ↓                      │
│        │            ┌─────────────┐            │
│        │            │  Consumer   │            │
│        │            │  flux_cdc.log           │
│        │            └─────────────┘            │
│        │                                       │
│  ┌──────────────────────────────────┐         │
│  │     KV Server Cluster            │         │
│  ├──────────────────────────────────┤         │
│  │  [Server-1] [Server-2] [Server-3]│         │
│  │   :50052     :50052     :50052   │         │
│  └──────────────────────────────────┘         │
│        ↑              ↑              ↑         │
│        └──────────────┼──────────────┘         │
│                       │                        │
│              ┌────────────────┐               │
│              │    Gateway     │               │
│              │   :8080        │               │
│              └────────────────┘               │
│                       │                        │
│              http://localhost:8080             │
└─────────────────────────────────────────────────┘
```

---

## 🔄 常见操作

### 查看容器日志

```bash
# 所有服务
docker-compose logs -f

# 特定服务
docker-compose logs -f kv-server-1
docker-compose logs -f gateway
docker-compose logs -f cdc-consumer

# 最后 100 行日志
docker-compose logs --tail=100
```

### 重启单个服务

```bash
# 重启 Server-1
docker-compose restart kv-server-1

# 重启 Gateway
docker-compose restart gateway
```

### 进入容器执行命令

```bash
# 进入 Server-1 容器的 Shell
docker exec -it flux-kv-server-1 sh

# 查看 AOF 文件
docker exec flux-kv-server-1 cat /app/data/kv-server-1.aof

# 检查 Etcd 注册信息
docker exec flux-kv-server-1 sh -c \
  "etcdctl --endpoints=etcd:2379 get /services/kv-service --prefix"
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

## ✅ 功能验证清单

### 1. 基础连通性

```bash
✓ 健康检查
curl http://localhost:8080/health
# {"status":"ok","message":"pong"}

✓ 写入测试数据
curl -X POST http://localhost:8080/api/v1/kv \
  -d '{"key":"test1","value":"value1"}'
# HTTP 200

✓ 读取数据
curl http://localhost:8080/api/v1/kv?key=test1
# {"key":"test1","value":"value1"}
```

### 2. 集群负载均衡

```bash
# 连续 10 次写入，应该分散到 3 个 Server
for i in {1..10}; do
  curl -s -X POST http://localhost:8080/api/v1/kv \
    -d "{\"key\":\"lb-test-$i\",\"value\":\"$i\"}" \
    -H "Content-Type: application/json"
done

# 查看日志确认负载分散
docker-compose logs --tail=50 | grep -E "shard|GET|SET"
```

### 3. 数据持久化

```bash
# 写入数据
curl -X POST http://localhost:8080/api/v1/kv \
  -d '{"key":"persist-test","value":"before-restart"}'

# 重启 Server-1
docker-compose restart kv-server-1
sleep 10

# 数据应该仍然存在（从 AOF 恢复）
curl http://localhost:8080/api/v1/kv?key=persist-test
# {"key":"persist-test","value":"before-restart"}
```

### 4. CDC 事件流

```bash
# 查看 CDC 消费者日志
docker exec flux-cdc-consumer cat /app/logs/flux_cdc.log | tail -20

# 应该看到 SET/DEL 事件的 JSON 格式
```

### 5. 链路追踪

```bash
# 生成追踪数据
for i in {1..30}; do
  curl -s -X POST http://localhost:8080/api/v1/kv \
    -d "{\"key\":\"trace-$i\",\"value\":\"$i\"}"
done

# 打开 http://localhost:16686
# 选择 Service: kv-service
# 应该看到 30 条 Trace，每条显示 Gateway → Server 的调用链
```

---

## 🔐 安全建议

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
   # 生产环境不应暴露 6060 端口
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
     kv-server-1:
       deploy:
         resources:
           limits:
             cpus: '0.5'
             memory: 512M
           reservations:
             cpus: '0.25'
             memory: 256M
   ```

---

## 🐛 故障排查

### 容器无法启动

```bash
# 查看详细错误日志
docker-compose logs <service-name>

# 例子：Server 启动失败
docker-compose logs kv-server-1

# 检查依赖服务是否就绪
docker-compose ps
```

### 服务间无法通信

```bash
# 检查网络连接
docker exec flux-gateway ping kv-server-1

# 应该得到正常的 PING 响应（说明 DNS 解析正常）
```

### Etcd 注册失败

```bash
# 检查 Etcd 是否运行
docker exec flux-etcd etcdctl endpoint health

# 检查 Server 是否成功注册
docker exec flux-etcd etcdctl get /services/kv-service --prefix

# 如果没有注册信息，检查 Server 日志
docker-compose logs kv-server-1 | grep -i etcd
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

---

## 📈 性能监控

### 使用 Pprof 分析性能

启用 Pprof（仅用于开发/测试）：

```bash
# 编辑 docker-compose.yaml，修改 Gateway 环境变量
- FLUX_PPROF_ENABLED=true

# 重启 Gateway
docker-compose restart gateway

# 访问性能分析
curl http://localhost:6060/debug/pprof

# 生成 CPU 火焰图
go tool pprof -http=:8001 http://localhost:6060/debug/pprof/profile?seconds=30
```

### 查看内存使用

```bash
# Docker 自带的内存监控
docker stats flux-kv-server-1 flux-gateway flux-rabbitmq

# 实时更新（按 Ctrl+C 退出）
```

---

## 🔄 升级和扩展

### 添加第 4 个 Server

```yaml
# 编辑 docker-compose.yaml，复制 kv-server-3 并修改：
kv-server-4:
  build:
    context: .
    dockerfile: Dockerfile.server
  container_name: flux-kv-server-4
  hostname: kv-server-4
  environment:
    - FLUX_ETCD_ENDPOINTS=etcd:2379
    - FLUX_RABBITMQ_URL=amqp://fluxadmin:flux2026secure@rabbitmq:5672/
    - FLUX_JAEGER_ENDPOINT=jaeger:4317
    - FLUX_AOF_FILENAME=/app/data/kv-server-4.aof
    - FLUX_PPROF_ENABLED=false
    - FLUX_POD_IP=kv-server-4
  ports:
    - "50055:50052"
  volumes:
    - kv-server-4-data:/app/data
  networks:
    - flux-net
  depends_on:
    etcd:
      condition: service_healthy
    rabbitmq:
      condition: service_healthy
  restart: unless-stopped

# 然后添加对应的 Volume
volumes:
  kv-server-4-data:
```

重启：
```bash
docker-compose up -d kv-server-4
```

### 添加 Prometheus 监控（未来计划）

未来会添加 Prometheus + Grafana 监控栈。

---

## 📝 数据备份和恢复

### 备份 AOF 文件

```bash
# 备份所有 Volume
for i in 1 2 3; do
  docker run --rm -v flux-kv_kv-server-${i}-data:/data \
    -v $(pwd)/backups:/backup alpine \
    tar czf /backup/server-${i}-$(date +%Y%m%d-%H%M%S).tar.gz -C /data .
done
```

### 恢复 AOF 文件

```bash
# 停止容器
docker-compose stop

# 清空 Volume（谨慎！）
docker volume rm flux-kv_kv-server-1-data

# 恢复数据
docker run --rm -v flux-kv_kv-server-1-data:/data \
  -v $(pwd)/backups:/backup alpine \
  tar xzf /backup/server-1-backup.tar.gz -C /data

# 重启容器
docker-compose start
```

---

## 📞 常见问题（FAQ）

**Q: 如何修改 Gateway 的 HTTP 端口为 8888？**

A: 编辑 docker-compose.yaml，修改 Gateway 的 `ports` 部分：
```yaml
ports:
  - "8888:8080"  # 宿主机 8888 → 容器 8080
```

**Q: 能否在生产环境直接使用这个 Compose 文件？**

A: 需要以下调整：
1. 修改所有密码
2. 禁用 Pprof
3. 添加资源限制
4. 改用 Kubernetes 而不是 Docker Compose

**Q: 如何扩展到 10 个 Server？**

A: 编辑 docker-compose.yaml，复制 kv-server-3 块 7 次，修改端口号和 AOF 文件名。

**Q: 支持跨主机部署吗？**

A: Docker Compose 本身仅支持单机。需要改用 Swarm 或 Kubernetes。

---

## 🚀 相关文档

- [go.mod 依赖说明](../go.mod)
- [架构设计文档](../README.md)

---

**最后更新**：2026-02-09
