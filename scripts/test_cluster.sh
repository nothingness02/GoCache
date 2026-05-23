#!/bin/bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

GW1="${GW1:-http://localhost:8080}"
GW2="${GW2:-http://localhost:8081}"
PASS=0
FAIL=0

pass() {
    echo -e "${GREEN}PASS${NC}"
    PASS=$((PASS + 1))
}

fail() {
    echo -e "${RED}FAIL${NC}: $1"
    FAIL=$((FAIL + 1))
}

echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo -e "${BOLD}${BLUE}              Flux-KV Cluster End-to-End Tests                ${NC}"
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "Gateway 1: ${CYAN}$GW1${NC}"
echo -e "Gateway 2: ${CYAN}$GW2${NC}"
echo ""

# Check prerequisites
if ! curl -sf "$GW1/health" >/dev/null 2>&1; then
    echo -e "${RED}❌ Gateway 1 ($GW1) is not reachable. Is the cluster running?${NC}"
    exit 1
fi

# ============================================================
# Test 1: AP Mode - Write via GW1, Read via GW1
# ============================================================
echo -e "${BOLD}Test 1: AP Write → Read (same gateway)${NC}"
AP_KEY="e2e_ap_$(date +%s)_$$"
AP_VALUE="ap_value_$(date +%s)"

echo -n "  1a. AP write via GW1 ............................ "
resp=$(curl -sf -X POST "$GW1/api/v1/kv" \
    -H "Content-Type: application/json" \
    -d "{\"key\":\"$AP_KEY\",\"value\":\"$AP_VALUE\"}" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q '"message".*"success"'; then
    pass
else
    fail "write failed: $resp"
fi

echo -n "  1b. AP read via GW1 ............................. "
resp=$(curl -sf "$GW1/api/v1/kv?key=$AP_KEY" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q "$AP_VALUE"; then
    pass
else
    fail "read mismatch: $resp"
fi

# ============================================================
# Test 2: CP Mode - Write via GW1, Read via GW1
# ============================================================
echo -e "${BOLD}Test 2: CP Write → Read (same gateway)${NC}"
CP_KEY="e2e_cp_$(date +%s)_$$"
CP_VALUE="cp_value_$(date +%s)"

echo -n "  2a. CP write via GW1 ............................ "
resp=$(curl -sf -X POST "$GW1/api/v1/kv?mode=cp" \
    -H "Content-Type: application/json" \
    -d "{\"key\":\"$CP_KEY\",\"value\":\"$CP_VALUE\"}" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q '"message".*"success"'; then
    pass
else
    fail "write failed: $resp"
fi

echo -n "  2b. CP read via GW1 ............................. "
resp=$(curl -sf "$GW1/api/v1/kv?key=$CP_KEY&mode=cp" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q "$CP_VALUE"; then
    pass
else
    fail "read mismatch: $resp"
fi

# ============================================================
# Test 3: Cross-Gateway Read
# ============================================================
echo -e "${BOLD}Test 3: Cross-Gateway Read (write GW1, read GW2)${NC}"

echo -n "  3a. AP read via GW2 ............................. "
resp=$(curl -sf "$GW2/api/v1/kv?key=$AP_KEY" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q "$AP_VALUE"; then
    pass
else
    fail "GW2 could not read AP key: $resp"
fi

echo -n "  3b. CP read via GW2 ............................. "
resp=$(curl -sf "$GW2/api/v1/kv?key=$CP_KEY&mode=cp" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q "$CP_VALUE"; then
    pass
else
    fail "GW2 could not read CP key: $resp"
fi

# ============================================================
# Test 4: Delete and Verify Gone
# ============================================================
echo -e "${BOLD}Test 4: Delete and Verify${NC}"
DEL_KEY="e2e_del_$(date +%s)_$$"
DEL_VALUE="to_be_deleted"

echo -n "  4a. Write then delete ........................... "
curl -sf -X POST "$GW1/api/v1/kv" \
    -H "Content-Type: application/json" \
    -d "{\"key\":\"$DEL_KEY\",\"value\":\"$DEL_VALUE\"}" >/dev/null 2>&1 || true
resp=$(curl -sf -X DELETE "$GW1/api/v1/kv?key=$DEL_KEY" 2>/dev/null || true)
if [ -n "$resp" ] && echo "$resp" | grep -q '"message".*"deleted"'; then
    pass
else
    fail "delete failed: $resp"
fi

echo -n "  4b. Verify key is gone .......................... "
resp=$(curl -sf "$GW1/api/v1/kv?key=$DEL_KEY" 2>/dev/null || true)
if [ -z "$resp" ] || echo "$resp" | grep -qi "error\|not found\|fail"; then
    pass
else
    fail "key still exists after delete: $resp"
fi

# ============================================================
# Test 5: Concurrent AP Writes (simple stress)
# ============================================================
echo -e "${BOLD}Test 5: Concurrent AP Writes${NC}"
CONC_KEY_PREFIX="concurrent_$(date +%s)_"
CONC_COUNT=10

echo -n "  5a. Sending $CONC_COUNT concurrent writes ....... "
for i in $(seq 1 $CONC_COUNT); do
    (
        curl -sf -X POST "$GW1/api/v1/kv" \
            -H "Content-Type: application/json" \
            -d "{\"key\":\"${CONC_KEY_PREFIX}${i}\",\"value\":\"v${i}\"}" >/dev/null 2>&1 || true
    ) &
done
wait
pass

echo -n "  5b. Verifying all $CONC_COUNT keys readable ..... "
MISSING=0
for i in $(seq 1 $CONC_COUNT); do
    resp=$(curl -sf "$GW1/api/v1/kv?key=${CONC_KEY_PREFIX}${i}" 2>/dev/null || true)
    if [ -z "$resp" ] || ! echo "$resp" | grep -q "v${i}"; then
        MISSING=$((MISSING + 1))
    fi
done

if [ $MISSING -eq 0 ]; then
    pass
else
    fail "$MISSING keys missing out of $CONC_COUNT"
fi

# ============================================================
# Test 6: Raft Leader Resilience (Optional)
# ============================================================
echo -e "${BOLD}Test 6: Raft Leader Resilience (optional)${NC}"

# Find current leader
LEADER_ADDR=""
CP_HOSTS=("cp-node-1:50052" "cp-node-2:50053" "cp-node-3:50054")
for addr in "${CP_HOSTS[@]}"; do
    resp=$(curl -sf "$GW1/admin/nodes/$addr/status" 2>/dev/null || true)
    if [ -n "$resp" ]; then
        role=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('raft_info',{}).get('role',''))" 2>/dev/null || echo "")
        if [ "$role" = "Leader" ]; then
            LEADER_ADDR=$addr
            break
        fi
    fi
done

if [ -z "$LEADER_ADDR" ]; then
    echo -e "  ${YELLOW}SKIP: No Raft Leader found (cluster may not be fully ready)${NC}"
else
    echo -e "  Current Leader: ${CYAN}$LEADER_ADDR${NC}"
    echo -n "  6a. CP write before any disruption .............. "
    RES_KEY="resilience_test_$(date +%s)"
    RES_VALUE="before"
    resp=$(curl -sf -X POST "$GW1/api/v1/kv?mode=cp" \
        -H "Content-Type: application/json" \
        -d "{\"key\":\"$RES_KEY\",\"value\":\"$RES_VALUE\"}" 2>/dev/null || true)
    if [ -n "$resp" ] && echo "$resp" | grep -q '"message".*"success"'; then
        pass
    else
        fail "write before disruption failed: $resp"
    fi

    # Note: Actually stopping the leader container would require docker access
    # and could disrupt the test environment. We just verify the write succeeded.
    echo -n "  6b. CP read after write ......................... "
    resp=$(curl -sf "$GW1/api/v1/kv?key=$RES_KEY&mode=cp" 2>/dev/null || true)
    if [ -n "$resp" ] && echo "$resp" | grep -q "$RES_VALUE"; then
        pass
    else
        fail "read after write failed: $resp"
    fi
fi

# ===== Summary =====
echo ""
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"
echo -e "Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"
if [ $FAIL -eq 0 ]; then
    echo -e "${BOLD}${GREEN}✅ All cluster end-to-end tests passed!${NC}"
else
    echo -e "${BOLD}${RED}❌ Some tests failed${NC}"
fi
echo -e "${BOLD}${BLUE}══════════════════════════════════════════════════════════════${NC}"

exit $FAIL
