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

FORCE=0
if [ "$1" = "--force" ] || [ "$1" = "-f" ]; then
    FORCE=1
fi

echo -e "${RED}⚠️  This will delete ALL persistent data:${NC}"
echo "   - AOF files (KV data)"
echo "   - Etcd registration data"
echo "   - RabbitMQ messages"
echo "   - CDC logs"
echo "   - Prometheus targets"
echo ""

if [ $FORCE -eq 0 ]; then
    read -p "Continue? (yes/NO): " -r
    if [[ $REPLY != "yes" ]]; then
        echo -e "${GREEN}❌ Cancelled${NC}"
        exit 0
    fi
fi

echo -e "${BLUE}🧹 Cleaning up...${NC}"
docker-compose down -v --remove-orphans

# Also remove any dangling flux images if requested
if [ "$1" = "--force" ] || [ "$1" = "-f" ]; then
    DANGLING=$(docker images -f "dangling=true" -q 2>/dev/null || true)
    if [ -n "$DANGLING" ]; then
        echo -e "${BLUE}🧹 Removing dangling images...${NC}"
        docker rmi $DANGLING 2>/dev/null || true
    fi
fi

echo ""
echo -e "${BLUE}🔍 Post-clean verification...${NC}"

# Check for remaining flux containers
REMAINING_CONTAINERS=$(docker ps -a --filter "name=flux-" --format "{{.Names}}" 2>/dev/null || true)
if [ -n "$REMAINING_CONTAINERS" ]; then
    echo -e "${YELLOW}⚠️  Remaining containers:${NC}"
    echo "$REMAINING_CONTAINERS" | sed 's/^/  - /'
else
    echo -e "${GREEN}✅ No Flux-KV containers remaining${NC}"
fi

# Check for remaining flux volumes
REMAINING_VOLUMES=$(docker volume ls -q --filter "name=flux-" 2>/dev/null || true)
if [ -n "$REMAINING_VOLUMES" ]; then
    echo -e "${YELLOW}⚠️  Remaining volumes:${NC}"
    echo "$REMAINING_VOLUMES" | sed 's/^/  - /'
else
    echo -e "${GREEN}✅ No Flux-KV volumes remaining${NC}"
fi

# Check for flux network
if docker network ls | grep -q "flux-net"; then
    echo -e "${YELLOW}⚠️  Network 'flux-net' still exists${NC}"
else
    echo -e "${GREEN}✅ Network 'flux-net' removed${NC}"
fi

echo ""
echo -e "${GREEN}✅ Clean complete${NC}"
