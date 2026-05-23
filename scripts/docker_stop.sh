#!/bin/bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

STOP_TIMEOUT=${STOP_TIMEOUT:-30}

echo -e "${BLUE}🛑 Stopping Flux-KV Cluster (graceful shutdown, timeout=${STOP_TIMEOUT}s)...${NC}"

# Layered shutdown: cut traffic first, then data, then infra

stop_layer() {
    local layer_name=$1
    shift
    local services=("$@")

    echo -e "${YELLOW}⏳ Stopping $layer_name...${NC}"
    docker-compose stop -t "$STOP_TIMEOUT" "${services[@]}" 2>/dev/null || true

    # Verify they are actually stopped
    local all_stopped=1
    for svc in "${services[@]}"; do
        local container_name
        container_name=$(docker-compose ps -q "$svc" 2>/dev/null || true)
        if [ -n "$container_name" ]; then
            local state
            state=$(docker inspect -f '{{.State.Status}}' "$container_name" 2>/dev/null || echo "missing")
            if [ "$state" = "running" ]; then
                echo -e "${RED}  ❌ $svc is still running${NC}"
                all_stopped=0
            else
                echo -e "${GREEN}  ✅ $svc stopped${NC}"
            fi
        else
            echo -e "${GREEN}  ✅ $svc not found (already stopped)${NC}"
        fi
    done

    return $((1 - all_stopped))
}

# Layer 1: Gateway (cut external traffic)
stop_layer "Gateway Layer" gateway-1 gateway-2

# Layer 2: Data / Monitor
stop_layer "Data & Monitor Layer" cdc-consumer prometheus-sd prometheus

# Layer 3: Storage nodes (CP + AP)
stop_layer "Storage Layer" cp-node-1 cp-node-2 cp-node-3 ap-node-1 ap-node-2

# Layer 4: Infrastructure
echo -e "${YELLOW}⏳ Stopping Infrastructure Layer...${NC}"
docker-compose stop -t "$STOP_TIMEOUT" etcd rabbitmq jaeger 2>/dev/null || true

# Final down to remove containers
echo -e "${BLUE}🧹 Removing containers...${NC}"
docker-compose down

# Verify nothing is left
echo ""
echo -e "${BLUE}🔍 Checking for remaining Flux-KV containers...${NC}"
REMAINING=$(docker ps -a --filter "name=flux-" --format "{{.Names}}\t{{.Status}}")
if [ -n "$REMAINING" ]; then
    echo -e "${YELLOW}⚠️  Some containers are still present:${NC}"
    echo "$REMAINING" | while IFS=$'\t' read -r name status; do
        echo "  - $name ($status)"
    done
else
    echo -e "${GREEN}✅ All Flux-KV containers removed${NC}"
fi

echo ""
echo -e "${GREEN}✅ Cluster stopped (data volumes preserved)${NC}"
echo "   To restart:  ./scripts/docker_start.sh"
echo "   To clean all data: ./scripts/docker_clean.sh"
