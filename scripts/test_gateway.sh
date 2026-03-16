#!/bin/bash
set -e

# 1. 检查 Server 端口
if ! ss -tuln | grep -q 50051; then 
    echo "❌ 错误: gRPC Server (50051) 未运行. 请先检查 Server 终端."
    exit 1
fi
echo "✅ gRPC Server 检测到在线."

# 2. 启动 Gateway (后台)
echo "🚀 启动 Gateway..."
nohup ./bin/gateway > gateway.log 2>&1 &
GATEWAY_PID=$!
echo "Gateway PID: $GATEWAY_PID"

# 3. 等待启动
echo "⏳ 等待 Gateway 就绪 (5s)..."
sleep 5

# 4. 显示部分日志以确认启动
echo "--- Gateway Logs (Head) ---"
head -n 10 gateway.log
echo "---------------------------"

# 5. 执行测试
echo "🧪 执行写入测试..."
curl -v -X POST http://localhost:8080/api/v1/kv \
  -H "Content-Type: application/json" \
  -d '{"key": "test_etcd_strict", "value": "This works with Strict Etcd!"}' \
  || echo "❌ 写入失败"

echo -e "\n🧪 执行读取测试..."
curl -v "http://localhost:8080/api/v1/kv?key=test_etcd_strict" \
  || echo "❌ 读取失败"

echo ""

# 6. 清理
echo "🛑 停止 Gateway..."
kill $GATEWAY_PID || true
echo "✅ 完成"
