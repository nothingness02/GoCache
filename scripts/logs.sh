#!/bin/bash

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# Default settings
FOLLOW=0
SINCE=""
TAIL="100"
SERVICES=()

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -f|--follow)
            FOLLOW=1
            shift
            ;;
        --since)
            SINCE="$2"
            shift 2
            ;;
        --tail)
            TAIL="$2"
            shift 2
            ;;
        --all)
            TAIL="all"
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [options] [service1 service2 ...]"
            echo ""
            echo "Options:"
            echo "  -f, --follow      Follow log output in real-time"
            echo "  --since TIME      Show logs newer than a relative duration (e.g., 10m, 1h)"
            echo "  --tail N          Show last N lines (default: 100, use --all for all)"
            echo "  -h, --help        Show this help"
            echo ""
            echo "Services:"
            echo "  etcd, rabbitmq, jaeger"
            echo "  cp-node-1, cp-node-2, cp-node-3"
            echo "  ap-node-1, ap-node-2"
            echo "  gateway-1, gateway-2"
            echo "  cdc-consumer, prometheus-sd, prometheus"
            echo ""
            echo "Examples:"
            echo "  $0                    # Show last 100 lines of all services"
            echo "  $0 gateway-1          # Show gateway-1 logs"
            echo "  $0 cp-node-1 cp-node-2 cp-node-3  # Show all CP node logs"
            echo "  $0 -f gateway-1       # Follow gateway-1 logs"
            echo "  $0 --since 10m        # Show logs from last 10 minutes"
            exit 0
            ;;
        -*)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Run '$0 --help' for usage."
            exit 1
            ;;
        *)
            SERVICES+=("$1")
            shift
            ;;
    esac
done

# Build docker-compose service list
if [ ${#SERVICES[@]} -eq 0 ]; then
    # Default: all services
    SERVICES=(etcd rabbitmq jaeger cp-node-1 cp-node-2 cp-node-3 ap-node-1 ap-node-2 gateway-1 gateway-2 cdc-consumer prometheus-sd prometheus)
fi

# Build docker-compose logs args
ARGS=()
if [ $FOLLOW -eq 1 ]; then
    ARGS+=("--follow")
fi
if [ -n "$SINCE" ]; then
    ARGS+=("--since=$SINCE")
fi
if [ "$TAIL" != "all" ]; then
    ARGS+=("--tail=$TAIL")
fi

# If following with multiple services, we can't pipe through sed easily
# So just run docker-compose logs directly
if [ $FOLLOW -eq 1 ]; then
    echo -e "${BLUE}📜 Following logs for: ${CYAN}${SERVICES[*]}${NC}"
    echo -e "${YELLOW}(Press Ctrl+C to stop)${NC}"
    echo ""
    docker-compose logs "${ARGS[@]}" "${SERVICES[@]}"
else
    # Non-follow mode: capture output and highlight errors
    echo -e "${BLUE}📜 Showing last $TAIL lines for: ${CYAN}${SERVICES[*]}${NC}"
    echo ""
    docker-compose logs "${ARGS[@]}" "${SERVICES[@]}" 2>&1 | \
        sed -E \
            -e "s/(ERROR|error|panic|fatal|FATAL)/${RED}\1${NC}/g" \
            -e "s/(WARN|warn|warning|WARNING)/${YELLOW}\1${NC}/g" \
            -e "s/(INFO|info)/${GREEN}\1${NC}/g" \
            -e "s/^(flux-[^|]+)/${CYAN}\1${NC}/"
fi
