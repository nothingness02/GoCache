#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

GW_GRPC_PORT="${GW_GRPC_PORT:-50051}"
GW_ADMIN_PORT="${GW_ADMIN_PORT:-9096}"
PASS=0
FAIL=0

# Helper functions
pass() {
    echo -e "${GREEN}PASS${NC}"
    PASS=$((PASS + 1))
}

fail() {
    echo -e "${RED}FAIL${NC}: $1"
    FAIL=$((FAIL + 1))
}

echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${BLUE}              Flux-KV Gateway Smoke Tests (gRPC)              ${NC}"
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "Target Gateway gRPC: ${CYAN}localhost:$GW_GRPC_PORT${NC}"
echo -e "Target Gateway Admin: ${CYAN}http://localhost:$GW_ADMIN_PORT${NC}"
echo ""

# ===== Test 1: gRPC Port Connectivity =====
echo -n "Test 1/6  gRPC port connectivity ................ "
if command -v nc >/dev/null 2>&1; then
    if nc -z localhost "$GW_GRPC_PORT" 2>/dev/null; then
        pass
    else
        fail "gRPC port $GW_GRPC_PORT is not open"
    fi
else
    # fallback: check if any process is listening on the port
    if ss -tlnp 2>/dev/null | grep -q ":$GW_GRPC_PORT " || \
       netstat -tlnp 2>/dev/null | grep -q ":$GW_GRPC_PORT "; then
        pass
    else
        echo -e "${YELLOW}SKIP${NC} (nc/ss/netstat not available)"
    fi
fi

# ===== Test 2: Admin /resilience Endpoint =====
echo -n "Test 2/6  Admin resilience status ............... "
resp=$(curl -sf "http://localhost:$GW_ADMIN_PORT/admin/resilience" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q '"rate_limiter"'; then
    pass
else
    fail "admin/resilience did not return expected JSON"
fi

# ===== Test 3: Admin /nodes Endpoint =====
echo -n "Test 3/6  Admin nodes list ...................... "
resp=$(curl -sf "http://localhost:$GW_ADMIN_PORT/admin/nodes" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q '"nodes"'; then
    pass
else
    fail "admin/nodes did not return nodes list"
fi

# ===== Test 4: Prometheus Metrics Endpoint =====
echo -n "Test 4/6  Prometheus metrics .................... "
resp=$(curl -sf "http://localhost:$GW_ADMIN_PORT/metrics" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q 'grpc_requests_total'; then
    pass
else
    fail "/metrics did not contain gRPC metrics"
fi

# ===== Test 5: Admin /stats Endpoint =====
echo -n "Test 5/6  Cluster stats ......................... "
resp=$(curl -sf "http://localhost:$GW_ADMIN_PORT/admin/stats" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q '"nodes"'; then
    pass
else
    fail "admin/stats did not return cluster stats"
fi

# ===== Test 6: Resilience Metrics Presence =====
echo -n "Test 6/6  Resilience metrics exposed ............. "
resp=$(curl -sf "http://localhost:$GW_ADMIN_PORT/metrics" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q 'circuit_breaker_state'; then
    pass
else
    fail "circuit_breaker_state metric not found"
fi

# ===== Summary =====
echo ""
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo -e "Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
if [ $FAIL -eq 0 ]; then
    echo -e "${BOLD}${GREEN}✅ All gateway smoke tests passed!${NC}"
else
    echo -e "${BOLD}${RED}❌ Some tests failed${NC}"
fi
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${BLUE}💡 To test KV operations via gRPC, run:${NC}"
echo "  go run ./cmd/client/main.go           # interactive gRPC client"
echo "  go run ./cmd/benchmark/main.go -c 10 -n 1000 -mode ap"
echo ""

exit $FAIL
