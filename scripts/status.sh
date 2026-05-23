#!/bin/bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

EXIT_CODE=0
GW_PORT=8080

# Helper: test if gateway is reachable
gw_available() {
    curl -sf "http://localhost:$GW_PORT/health" >/dev/null 2>&1
}

# ===== 1. Container Status =====
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${BLUE}                    Flux-KV Cluster Status                    ${NC}"
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo ""

echo -e "${BOLD}📦 Docker Containers${NC}"
printf "${BOLD}%-24s %-12s %-12s %-20s${NC}\n" "NAME" "STATUS" "HEALTH" "UPTIME"
printf "%-24s %-12s %-12s %-20s\n" "------------------------" "------------" "------------" "--------------------"

CONTAINERS=$(docker ps -a --filter "name=flux-" --format "{{.Names}}" | sort)
if [ -z "$CONTAINERS" ]; then
    echo -e "${RED}No Flux-KV containers found${NC}"
    EXIT_CODE=1
else
    echo "$CONTAINERS" | while read -r name; do
        status=$(docker inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo "unknown")
        health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}N/A{{end}}' "$name" 2>/dev/null || echo "N/A")
        uptime=$(docker inspect -f '{{.State.StartedAt}}' "$name" 2>/dev/null | xargs -I {} date -d {} +"%Y-%m-%d %H:%M:%S" 2>/dev/null || echo "N/A")

        status_color="${NC}"
        if [ "$status" = "running" ]; then status_color="${GREEN}"; fi
        if [ "$status" = "exited" ] || [ "$status" = "dead" ]; then status_color="${RED}"; EXIT_CODE=1; fi

        health_color="${NC}"
        if [ "$health" = "healthy" ]; then health_color="${GREEN}"; fi
        if [ "$health" = "unhealthy" ]; then health_color="${RED}"; EXIT_CODE=1; fi

        printf "%-24s ${status_color}%-12s${NC} ${health_color}%-12s${NC} %-20s\n" "$name" "$status" "$health" "$uptime"
    done
fi

echo ""

# ===== 2. Gateway / Admin Stats =====
if gw_available; then
    echo -e "${BOLD}🌐 Gateway Admin Stats${NC}"
    STATS=$(curl -sf "http://localhost:$GW_PORT/admin/stats" 2>/dev/null || true)
    if [ -n "$STATS" ]; then
        cp_nodes=$(echo "$STATS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('cp_nodes',0))" 2>/dev/null || echo "?")
        ap_nodes=$(echo "$STATS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('ap_nodes',0))" 2>/dev/null || echo "?")
        total_entries=$(echo "$STATS" | python3 -c "import sys,json; print(json.load(sys.stdin).get('total_entries',0))" 2>/dev/null || echo "?")
        total_memory=$(echo "$STATS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('total_memory',0))" 2>/dev/null || echo "?")

        printf "  %-20s: %s\n" "CP Nodes Online" "$cp_nodes"
        printf "  %-20s: %s\n" "AP Nodes Online" "$ap_nodes"
        printf "  %-20s: %s\n" "Total KV Entries" "$total_entries"
        printf "  %-20s: %s bytes\n" "Total Memory" "$total_memory"

        # Node details
        echo ""
        printf "  ${BOLD}%-24s %-8s %-12s${NC}\n" "ADDR" "MODE" "STATUS"
        printf "  %-24s %-8s %-12s\n" "------------------------" "--------" "------------"
        echo "$STATS" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for n in data.get('nodes', []):
    addr = n.get('Addr', 'N/A')
    mode = n.get('Status', {}).get('Mode', 'unknown') if n.get('Status') else 'unknown'
    err = n.get('Error', '')
    status = 'OK' if n.get('Status') and not err else ('ERROR: ' + err if err else 'UNKNOWN')
    print(f'  {addr:24} {mode:8} {status}')
" 2>/dev/null || true
    else
        echo -e "  ${YELLOW}⚠️  Could not fetch admin stats${NC}"
        EXIT_CODE=1
    fi
else
    echo -e "${BOLD}🌐 Gateway${NC}"
    echo -e "  ${RED}❌ Gateway not reachable on port $GW_PORT${NC}"
    EXIT_CODE=1
fi

echo ""

# ===== 3. Raft Cluster Diagnosis =====
echo -e "${BOLD}🗳️  Raft Cluster (CP Nodes)${NC}"

CP_HOSTS=("cp-node-1:50052" "cp-node-2:50053" "cp-node-3:50054")
RAFT_LEADER_COUNT=0
RAFT_LEADER_ADDR=""

for addr in "${CP_HOSTS[@]}"; do
    if gw_available; then
        resp=$(curl -sf "http://localhost:$GW_PORT/admin/nodes/$addr/status" 2>/dev/null || true)
        if [ -n "$resp" ]; then
            role=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('raft_info',{}).get('role','N/A'))" 2>/dev/null || echo "N/A")
            term=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('raft_info',{}).get('term','N/A'))" 2>/dev/null || echo "N/A")
            commit=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('raft_info',{}).get('commit_index','N/A'))" 2>/dev/null || echo "N/A")
            mode=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('status',{}).get('Mode','N/A'))" 2>/dev/null || echo "N/A")

            role_color="${NC}"
            if [ "$role" = "Leader" ]; then
                role_color="${GREEN}"
                RAFT_LEADER_COUNT=$((RAFT_LEADER_COUNT + 1))
                RAFT_LEADER_ADDR=$addr
            elif [ "$role" = "Follower" ]; then
                role_color="${CYAN}"
            elif [ "$role" = "Candidate" ]; then
                role_color="${YELLOW}"
            fi

            printf "  %-20s ${role_color}%-10s${NC} term=%-4s commit=%-6s mode=%s\n" "$addr" "$role" "$term" "$commit" "$mode"
        else
            printf "  ${RED}%-20s %-10s${NC}\n" "$addr" "OFFLINE"
            EXIT_CODE=1
        fi
    else
        printf "  ${YELLOW}%-20s %-10s${NC}\n" "$addr" "UNKNOWN"
    fi
done

if [ $RAFT_LEADER_COUNT -eq 0 ]; then
    echo -e "  ${RED}❌ No Raft Leader found!${NC}"
    EXIT_CODE=1
elif [ $RAFT_LEADER_COUNT -gt 1 ]; then
    echo -e "  ${RED}❌ Multiple Raft Leaders detected ($RAFT_LEADER_COUNT)!${NC}"
    EXIT_CODE=1
else
    echo -e "  ${GREEN}✅ Raft Leader: $RAFT_LEADER_ADDR${NC}"
fi

echo ""

# ===== 4. Etcd Service Discovery =====
echo -e "${BOLD}📋 Etcd Service Registry${NC}"
ETCD_OUTPUT=$(docker exec flux-etcd etcdctl --endpoints=http://localhost:2379 get --prefix /services/kv-service/ 2>/dev/null || true)
if [ -n "$ETCD_OUTPUT" ]; then
    echo "$ETCD_OUTPUT" | grep "^/services/kv-service/" | while read -r key; do
        read -r value
        printf "  %-40s %s\n" "$key" "$value"
    done
else
    echo -e "  ${YELLOW}⚠️  Could not query Etcd (may not be running)${NC}"
fi

echo ""

# ===== 5. Summary Banner =====
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${BOLD}${GREEN}✅ Cluster status: HEALTHY${NC}"
else
    echo -e "${BOLD}${RED}❌ Cluster status: ISSUES DETECTED${NC}"
fi
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"

exit $EXIT_CODE
