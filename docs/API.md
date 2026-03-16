# API Reference

## 🌐 Base URL
`http://localhost:8080/api/v1`

## 🔐 Authentication
目前接口为公开访问（Public），但在网关层集成了 **Token Bucket 限流** 机制保护。

---

## 🔑 Key-Value Operations

### 1. Set Value (写入/更新)
将键值对存储到分布式集群中。此操作是**强一致性**写入内存，并**异步**触发 CDC 事件。

- **URL**: `/kv`
- **Method**: `POST`
- **Content-Type**: `application/x-www-form-urlencoded`

| Parameter | Type   | Required | Description       |
| :---      | :---   | :---     | :---              |
| `key`     | string | Yes      | 键名 (e.g. `user:1001`) |
| `value`   | string | Yes      | 键值 (e.g. `{"name": "wang"}`) |

**Success Response:**
```json
{
    "success": true
}
```

**Error Response:**
- `429 Too Many Requests`: 触发全局限流
- `503 Service Unavailable`: 触发熔断降级

---

### 2. Get Value (读取)
根据 Key 获取 Value。

- **URL**: `/kv`
- **Method**: `GET`
- **Query Params**:
    - `key`: 目标键名

**Example**:
```bash
curl "http://localhost:8080/api/v1/kv?key=user:1001"
```

**Response:**
```json
{
    "value": "{\"name\": \"wang\"}",
    "found": true
}
```

---

### 3. Delete Value (删除)
删除指定的 Key。

- **URL**: `/kv`
- **Method**: `DELETE`
- **Query Params**:
    - `key`: 目标键名

**Response:**
```json
{
    "success": true
}
```

---

## 🩺 System Check

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
