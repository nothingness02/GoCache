#!/bin/bash

# ================================================================
# Graceful Shutdown 自动化验证脚本
# ================================================================
# 功能：
# 1. 清理旧日志
# 2. 启动 Server 和 Consumer（后台）
# 3. 启动高并发 Client 发送 500 个 SET 请求
# 4. Client 运行到一半时（1秒后）向进程发送 SIGTERM
# 5. 等待进程退出
# 6. 验证关键日志和数据一致性
# ================================================================

set -e

# ================================================================
# 颜色定义
# ================================================================
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# ================================================================
# 日志函数
# ================================================================
log_info() {
  echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
  echo -e "${GREEN}[✓ PASS]${NC} $1"
}

log_fail() {
  echo -e "${RED}[✗ FAIL]${NC} $1"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

# ================================================================
# 配置参数
# ================================================================
WORKSPACE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$WORKSPACE_DIR"

SERVER_PORT=50052
ETCD_ADDR="localhost:2379"
RABBITMQ_URL="amqp://guest:guest@localhost:5672/"

BIN_DIR="$WORKSPACE_DIR/.verify_shutdown_bin"
SERVER_BIN="$BIN_DIR/server"
CONSUMER_BIN="$BIN_DIR/consumer"

# 日志文件
SERVER_LOG="$WORKSPACE_DIR/server_out.log"
CONSUMER_LOG="$WORKSPACE_DIR/consumer_out.log"
CLIENT_LOG="$WORKSPACE_DIR/client_out.log"

# 数据文件
AOF_FILE="$WORKSPACE_DIR/go-kv.aof"
CDC_LOG_FILE="$WORKSPACE_DIR/flux_cdc.log"

# 并发配置
TOTAL_REQUESTS=500
KILL_DELAY_SECONDS=1  # 1 秒后杀死进程

TIMEOUT_CMD=""
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout"
fi

cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill -TERM "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "${CONSUMER_PID:-}" ]; then
    kill -TERM "$CONSUMER_PID" 2>/dev/null || true
  fi
  rm -rf "$BIN_DIR"
}

trap cleanup EXIT

# ================================================================
# Step 1: 清理旧文件
# ================================================================
log_info "清理旧的日志和数据文件..."

rm -f "$SERVER_LOG" "$CONSUMER_LOG" "$CLIENT_LOG"
rm -f "$AOF_FILE" "$CDC_LOG_FILE"
rm -f *.log *.aof 2>/dev/null || true
rm -rf "$BIN_DIR"

log_success "清理完成"

# ================================================================
# Step 2: 杀死残留进程
# ================================================================
log_info "杀死可能残留的进程..."

pkill -f "go run cmd/server" || true
pkill -f "go run cmd/cdc_consumer" || true
pkill -f "go run cmd/client" || true
pkill -f "$SERVER_BIN" || true
pkill -f "$CONSUMER_BIN" || true

if command -v lsof >/dev/null 2>&1; then
  PORT_PIDS=$(lsof -ti tcp:"$SERVER_PORT" 2>/dev/null || true)
  if [ -n "$PORT_PIDS" ]; then
    log_warn "端口 $SERVER_PORT 被占用，尝试释放..."
    kill -TERM $PORT_PIDS 2>/dev/null || true
    sleep 1
  fi
elif command -v fuser >/dev/null 2>&1; then
  if fuser "$SERVER_PORT"/tcp >/dev/null 2>&1; then
    log_warn "端口 $SERVER_PORT 被占用，尝试释放..."
    fuser -k "$SERVER_PORT"/tcp >/dev/null 2>&1 || true
    sleep 1
  fi
fi

sleep 1

# ================================================================
# Step 3: 启动服务（Server 和 Consumer）
# ================================================================
log_info "编译 Server 和 Consumer..."

mkdir -p "$BIN_DIR"
go build -o "$SERVER_BIN" "$WORKSPACE_DIR/cmd/server/main.go"
go build -o "$CONSUMER_BIN" "$WORKSPACE_DIR/cmd/cdc_consumer/main.go"

log_success "编译完成"

log_info "启动 gRPC Server (端口: $SERVER_PORT)..."

"$SERVER_BIN" -port $SERVER_PORT >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
log_success "Server 已启动 (PID: $SERVER_PID)"

log_info "启动 CDC Consumer..."

"$CONSUMER_BIN" >"$CONSUMER_LOG" 2>&1 &
CONSUMER_PID=$!
log_success "Consumer 已启动 (PID: $CONSUMER_PID)"

# ================================================================
# Step 4: 等待服务就绪
# ================================================================
log_info "等待服务就绪 (2 秒)..."
sleep 2

# 验证进程是否还活着
if ! kill -0 $SERVER_PID 2>/dev/null; then
  log_fail "Server 启动失败"
  cat "$SERVER_LOG"
  exit 1
fi

if ! kill -0 $CONSUMER_PID 2>/dev/null; then
  log_fail "Consumer 启动失败"
  cat "$CONSUMER_LOG"
  exit 1
fi

log_success "所有服务已就绪"

# ================================================================
# Step 5: 启动 Client 并发送请求
# ================================================================
log_info "启动 Client 发送 $TOTAL_REQUESTS 个 SET 请求..."

# 定义发送请求的函数
send_requests() {
  local count=0
  
  # 生成 SET 命令列表
  for i in $(seq 1 $TOTAL_REQUESTS); do
    echo "set key_$i value_$i"
    count=$((count + 1))
  done
  
  # 最后退出
  echo "quit"
}

# 在后台启动 Client，管道输入请求，并记录输出
if [ -n "$TIMEOUT_CMD" ]; then
  send_requests | "$TIMEOUT_CMD" 30 go run "$WORKSPACE_DIR/cmd/client/main.go" -etcd "$ETCD_ADDR" >"$CLIENT_LOG" 2>&1 &
else
  send_requests | go run "$WORKSPACE_DIR/cmd/client/main.go" -etcd "$ETCD_ADDR" >"$CLIENT_LOG" 2>&1 &
fi
CLIENT_PID=$!

# ================================================================
# Step 6: Client 运行到一半时杀死进程
# ================================================================
log_info "等待 Client 产生成功请求..."

OK_WAIT=0
OK_TIMEOUT=5
while [ $OK_WAIT -lt $OK_TIMEOUT ]; do
  if grep -q "OK" "$CLIENT_LOG" 2>/dev/null; then
    break
  fi
  sleep 1
  OK_WAIT=$((OK_WAIT + 1))
done

if [ $OK_WAIT -ge $OK_TIMEOUT ]; then
  log_warn "未检测到成功请求，继续按时发送 SIGTERM"
fi

log_info "等待 $KILL_DELAY_SECONDS 秒..."
sleep "$KILL_DELAY_SECONDS"

log_warn "[$((TOTAL_REQUESTS/2)) 个请求后] 发送 SIGTERM 信号到 Server 和 Consumer..."

# 发送优雅关闭信号
kill -TERM $SERVER_PID 2>/dev/null || true
kill -TERM $CONSUMER_PID 2>/dev/null || true

# ================================================================
# Step 7: 等待进程退出
# ================================================================
log_info "等待进程优雅退出..."

# 给进程 10 秒时间优雅退出
TIMEOUT=10
ELAPSED=0

while [ $ELAPSED -lt $TIMEOUT ]; do
  SERVER_ALIVE=false
  CONSUMER_ALIVE=false
  
  kill -0 $SERVER_PID 2>/dev/null && SERVER_ALIVE=true || true
  kill -0 $CONSUMER_PID 2>/dev/null && CONSUMER_ALIVE=true || true
  
  if ! $SERVER_ALIVE && ! $CONSUMER_ALIVE; then
    log_success "Server 和 Consumer 已退出"
    break
  fi
  
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

# 如果超时，强制杀死
if kill -0 $SERVER_PID 2>/dev/null; then
  log_warn "Server 在超时后被强制杀死"
  kill -9 $SERVER_PID 2>/dev/null || true
fi

if kill -0 $CONSUMER_PID 2>/dev/null; then
  log_warn "Consumer 在超时后被强制杀死"
  kill -9 $CONSUMER_PID 2>/dev/null || true
fi

# 让 Client 继续完成或超时
wait $CLIENT_PID 2>/dev/null || true

sleep 1

# ================================================================
# Step 8: 验证日志
# ================================================================
echo ""
echo "════════════════════════════════════════════════════════════"
echo "验证结果"
echo "════════════════════════════════════════════════════════════"
echo ""

PASS_COUNT=0
FAIL_COUNT=0

# 检查 Server 日志
log_info "检查 Server 日志..."

if grep -q "正在注销 Etcd" "$SERVER_LOG"; then
  log_success "✓ Server 正在注销 Etcd"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  log_fail "✗ Server 没有注销 Etcd"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

if grep -q "MemDB 数据已安全落袋" "$SERVER_LOG"; then
  log_success "✓ MemDB 数据已安全落袋"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  log_fail "✗ MemDB 数据未安全落袋"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

# 检查 Consumer 日志
log_info "检查 Consumer 日志..."

if grep -q "Consumer 安全退出\|CDC Consumer 安全退出" "$CONSUMER_LOG"; then
  log_success "✓ Consumer 安全退出"
  PASS_COUNT=$((PASS_COUNT + 1))
else
  log_fail "✗ Consumer 未安全退出"
  FAIL_COUNT=$((FAIL_COUNT + 1))
fi

echo ""

# ================================================================
# Step 9: 统计数据一致性
# ================================================================
log_info "统计数据一致性..."

# 从 Client 日志统计成功的请求数
CLIENT_SUCCESS=$(grep -c "OK" "$CLIENT_LOG" 2>/dev/null || true)
log_info "Client 发送成功的 SET 请求: $CLIENT_SUCCESS"

# AOF 文件行数
if [ -f "$AOF_FILE" ]; then
  AOF_LINES=$(wc -l < "$AOF_FILE" 2>/dev/null || echo 0)
  log_info "AOF 文件写入的命令: $AOF_LINES 条"
else
  AOF_LINES=0
  log_warn "AOF 文件不存在"
fi

# CDC 日志行数
if [ -f "$CDC_LOG_FILE" ]; then
  CDC_LINES=$(grep -c "CDC_SYNC" "$CDC_LOG_FILE" 2>/dev/null || true)
  log_info "CDC 日志写入的事件: $CDC_LINES 条"
else
  CDC_LINES=0
  log_warn "CDC 日志文件不存在"
fi

echo ""

# ================================================================
# Step 10: 数据对比报告
# ================================================================
log_info "数据对比报告..."

echo ""
echo "┌────────────────────────────────────────────────────────┐"
echo "│ 数据一致性验证                                          │"
echo "├────────────────────────────────────────────────────────┤"
echo "│ Client 成功请求数:    $CLIENT_SUCCESS"
echo "│ AOF 文件写入数:       $AOF_LINES"
echo "│ CDC 日志事件数:       $CDC_LINES"
echo "├────────────────────────────────────────────────────────┤"

# 检查数据一致性
if [ $CLIENT_SUCCESS -gt 0 ]; then
  if [ $AOF_LINES -eq $CLIENT_SUCCESS ]; then
    echo "│ ✓ AOF 与 Client 请求一致"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "│ ✗ AOF 与 Client 请求不一致 (差异: $((CLIENT_SUCCESS - AOF_LINES)))"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
else
  log_warn "Client 没有成功的请求，跳过 AOF 对比"
fi

if [ $CLIENT_SUCCESS -gt 0 ] && [ $CDC_LINES -gt 0 ]; then
  if [ $CDC_LINES -eq $CLIENT_SUCCESS ]; then
    echo "│ ✓ CDC 与 Client 请求一致"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    # CDC 事件数可能因为异步而少于请求数，这是正常的
    if [ $CDC_LINES -le $CLIENT_SUCCESS ]; then
      echo "│ ⚠ CDC 事件少于 Client 请求 (可能未完全处理，差异: $((CLIENT_SUCCESS - CDC_LINES)))"
    else
      echo "│ ✗ CDC 事件异常多于 Client 请求"
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
  fi
fi

echo "└────────────────────────────────────────────────────────┘"

echo ""

# ================================================================
# Step 11: 最终结果统计
# ================================================================
echo "════════════════════════════════════════════════════════════"
echo "最终结果"
echo "════════════════════════════════════════════════════════════"

TOTAL=$((PASS_COUNT + FAIL_COUNT))

if [ $FAIL_COUNT -eq 0 ]; then
  echo -e "${GREEN}✓ 所有验证通过 ($PASS_COUNT/$TOTAL)${NC}"
  EXIT_CODE=0
else
  echo -e "${RED}✗ 有 $FAIL_COUNT 项验证失败 (通过: $PASS_COUNT, 失败: $FAIL_COUNT)${NC}"
  EXIT_CODE=1
fi

echo ""

# ================================================================
# Step 12: 打印详细日志（用于调试）
# ================================================================
log_info "详细日志位置:"
echo "  Server 日志:    $SERVER_LOG"
echo "  Consumer 日志:  $CONSUMER_LOG"
echo "  Client 日志:    $CLIENT_LOG"
echo "  AOF 文件:       $AOF_FILE"
echo "  CDC 日志:       $CDC_LOG_FILE"

echo ""

# 可选：显示关键日志片段
if [ -f "$SERVER_LOG" ] && [ $EXIT_CODE -ne 0 ]; then
  echo "════════════════════════════════════════════════════════════"
  echo "Server 日志片段:"
  echo "════════════════════════════════════════════════════════════"
  tail -20 "$SERVER_LOG"
  echo ""
fi

if [ -f "$CONSUMER_LOG" ] && [ $EXIT_CODE -ne 0 ]; then
  echo "════════════════════════════════════════════════════════════"
  echo "Consumer 日志片段:"
  echo "════════════════════════════════════════════════════════════"
  tail -20 "$CONSUMER_LOG"
  echo ""
fi

exit $EXIT_CODE
