#!/bin/bash
set -e

# 检查 .env 文件
if [ ! -f .env ]; then
    echo "⚠️  .env file not found! Creating default..."
    cat > .env <<EOF
RABBITMQ_USER=fluxadmin
RABBITMQ_PASS=flux2026secure
EOF
    echo "✅ Created .env with default credentials"
fi

echo "🔨 Building Docker images..."
docker-compose build --parallel

echo "🚀 Starting Flux-KV Cluster..."
docker-compose up -d

echo "⏳ Waiting for services to be healthy..."
sleep 15

echo ""
echo "✅ Cluster Status:"
docker-compose ps

echo ""
echo "📊 Access Points:"
echo "  Gateway API:       http://localhost:8080"
echo "  Health Check:      http://localhost:8080/health"
echo "  Jaeger UI:         http://localhost:16686"
echo "  RabbitMQ Console:  http://localhost:15672 (user: fluxadmin)"
echo "  Etcd:              http://localhost:2379"

echo ""
echo "🔍 Quick Test:"
echo "  curl http://localhost:8080/health"
echo "  curl -X POST http://localhost:8080/api/v1/kv -H 'Content-Type: application/json' -d '{\"key\":\"test\",\"value\":\"hello\"}'"
echo "  curl http://localhost:8080/api/v1/kv?key=test"
