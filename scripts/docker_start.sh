#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# ===== 1. Environment Setup =====
if [ ! -f .env ]; then
    echo -e "${YELLOW}⚠️  .env file not found! Creating default...${NC}"
    cat > .env <<EOF
RABBITMQ_USER=fluxadmin
RABBITMQ_PASS=flux2026secure
EOF
    echo -e "${GREEN}✅ Created .env with default credentials${NC}"
fi

# ===== 2. Build Images =====
echo -e "${BLUE}🔨 Building Docker images...${NC}"
docker-compose build --parallel

# ===== 3. Start Services =====
echo -e "${BLUE}🚀 Starting Flux-KV Cluster...${NC}"
docker-compose up -d

# ===== 4. Wait for Services =====

wait_for_container() {
    local name=$1
    local max_wait=${2:-60}
    local waited=0

    echo -n "⏳ Waiting for $name..."

    while [ $waited -lt $max_wait ]; do
        local state
        state=$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo "missing")

        if [ "$state" = "missing" ]; then
            sleep 1
            waited=$((waited + 1))
            continue
        fi

        if [ "$state" != "running" ]; then
            echo -e "\n${RED}❌ $name is in state: $state${NC}"
            return 1
        fi

        # Check health if available
        local has_health
        has_health=$(docker inspect -f '{{if .State.Health}}yes{{end}}' "$name" 2>/dev/null || echo "")

        if [ "$has_health" = "yes" ]; then
            local health_status
            health_status=$(docker inspect -f '{{.State.Health.Status}}' "$name" 2>/dev/null || echo "unknown")
            if [ "$health_status" = "healthy" ]; then
                echo -e " ${GREEN}✅ healthy (${waited}s)${NC}"
                return 0
            fi
        else
            # No healthcheck configured, running is enough
            echo -e " ${GREEN}✅ running (${waited}s)${NC}"
            return 0
        fi

        sleep 1
        waited=$((waited + 1))
        echo -n "."
    done

    echo -e "\n${RED}❌ $name did not become healthy within ${max_wait}s${NC}"
    return 1
}

# Infrastructure layer
INFRA_CONTAINERS=("flux-etcd" "flux-rabbitmq" "flux-jaeger")
for c in "${INFRA_CONTAINERS[@]}"; do
    wait_for_container "$c" 60
done

# Storage layer
STORAGE_CONTAINERS=("flux-cp-node-1" "flux-cp-node-2" "flux-cp-node-3" "flux-ap-node-1" "flux-ap-node-2")
for c in "${STORAGE_CONTAINERS[@]}"; do
    wait_for_container "$c" 60
done

# Gateway layer
GATEWAY_CONTAINERS=("flux-gateway-1" "flux-gateway-2")
for c in "${GATEWAY_CONTAINERS[@]}"; do
    wait_for_container "$c" 60
done

# Data/Monitor layer
OTHER_CONTAINERS=("flux-cdc-consumer" "flux-prometheus-sd" "flux-prometheus")
for c in "${OTHER_CONTAINERS[@]}"; do
    wait_for_container "$c" 60
done

# ===== 5. Verify Gateway Health =====
echo -e "${BLUE}🔍 Verifying Gateway health...${NC}"
GW_HEALTH_OK=0
for admin_port in 9096 9097; do
    for i in {1..30}; do
        if curl -sf http://localhost:$admin_port/admin/resilience >/dev/null 2>&1; then
            echo -e "${GREEN}✅ Gateway admin on port $admin_port is healthy${NC}"
            GW_HEALTH_OK=1
            break 2
        fi
        sleep 1
    done
done

if [ $GW_HEALTH_OK -eq 0 ]; then
    echo -e "${YELLOW}⚠️  Gateway health check did not pass, but services are running${NC}"
fi

# ===== 6. Verify Raft Election =====
echo -e "${BLUE}🗳️  Verifying Raft election...${NC}"
RAFT_LEADER_FOUND=0
RAFT_LEADER_ADDR=""

# CP node gRPC ports on localhost
CP_PORTS=(50052 50053 50054)
CP_HOSTS=("cp-node-1" "cp-node-2" "cp-node-3")

for attempt in {1..30}; do
    LEADER_COUNT=0
    LEADER_ADDR=""
    LEADER_TERM=0

    for i in "${!CP_PORTS[@]}"; do
        port=${CP_PORTS[$i]}
        # Try to get Raft info via gateway admin (gateway can reach cp-node-* inside docker network)
        resp=$(curl -sf "http://localhost:9096/admin/nodes/${CP_HOSTS[$i]}:$port/status" 2>/dev/null || true)
        if [ -n "$resp" ]; then
            role=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('raft_info',{}).get('role','unknown'))" 2>/dev/null || echo "unknown")
            term=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('raft_info',{}).get('term',0))" 2>/dev/null || echo "0")
            if [ "$role" = "Leader" ]; then
                LEADER_COUNT=$((LEADER_COUNT + 1))
                LEADER_ADDR="${CP_HOSTS[$i]}:$port"
                LEADER_TERM=$term
            fi
        fi
    done

    if [ $LEADER_COUNT -eq 1 ]; then
        RAFT_LEADER_FOUND=1
        RAFT_LEADER_ADDR=$LEADER_ADDR
        echo -e "${GREEN}✅ Raft Leader elected: $LEADER_ADDR (term $LEADER_TERM)${NC}"
        break
    elif [ $LEADER_COUNT -gt 1 ]; then
        echo -e "${YELLOW}⚠️  Multiple leaders detected ($LEADER_COUNT), waiting for convergence...${NC}"
    fi

    sleep 1
done

if [ $RAFT_LEADER_FOUND -eq 0 ]; then
    echo -e "${YELLOW}⚠️  Raft Leader not found after 30s (election may still be in progress)${NC}"
fi

# ===== 7. Summary =====
echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║          Flux-KV Cluster Started Successfully              ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

echo -e "${BLUE}📦 Container Status:${NC}"
printf "%-25s %-15s %-15s\n" "NAME" "STATUS" "PORTS"
printf "%-25s %-15s %-15s\n" "-------------------------" "---------------" "---------------"

for c in "${INFRA_CONTAINERS[@]}" "${STORAGE_CONTAINERS[@]}" "${GATEWAY_CONTAINERS[@]}" "${OTHER_CONTAINERS[@]}"; do
    status=$(docker inspect -f '{{.State.Status}}' "$c" 2>/dev/null || echo "N/A")
    ports=$(docker inspect -f '{{range $p, $conf := .NetworkSettings.Ports}}{{if $conf}}{{range $conf}}{{$p}}->{{.HostPort}} {{end}}{{end}}{{end}}' "$c" 2>/dev/null | head -c 40 || echo "N/A")
    printf "%-25s %-15s %-15s\n" "$c" "$status" "$ports"
done

echo ""
echo -e "${BLUE}🔗 Access Points:${NC}"
echo "  Gateway gRPC:       localhost:50051"
echo "  Gateway gRPC (alt): localhost:50052"
echo "  Gateway Admin:      http://localhost:9096/admin/resilience"
echo "  Admin Nodes:        http://localhost:9096/admin/nodes"
echo "  Admin Stats:        http://localhost:9096/admin/stats"
echo "  Jaeger UI:          http://localhost:16686"
echo "  RabbitMQ Console:   http://localhost:15672 (user: fluxadmin)"
echo "  Etcd:               localhost:2379"
echo "  Prometheus:         http://localhost:9090"
echo ""

echo -e "${BLUE}🧪 Quick Tests:${NC}"
echo "  curl http://localhost:9096/admin/resilience"
echo "  curl http://localhost:9096/admin/nodes"
echo "  curl http://localhost:9096/admin/stats"
echo "  go run ./cmd/client/main.go   # interactive gRPC client"
echo "  go run ./cmd/benchmark/main.go -c 10 -n 1000 -mode ap"
echo ""

echo -e "${BLUE}📋 Diagnostics:${NC}"
echo "  ./scripts/status.sh    # Full cluster status"
echo "  ./scripts/logs.sh      # View all logs"
echo "  ./scripts/test_gateway.sh  # Run API smoke tests"
echo ""
