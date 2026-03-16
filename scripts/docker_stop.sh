#!/bin/bash

echo "🛑 Stopping Flux-KV Cluster (graceful shutdown)..."
docker-compose stop -t 30  # 30秒优雅停止时间

echo "🧹 Removing containers..."
docker-compose down

echo "✅ Cluster stopped (data volumes preserved)"
echo "   To restart: ./scripts/docker_start.sh"
