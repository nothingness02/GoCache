#!/bin/bash

echo "⚠️  This will delete ALL persistent data:"
echo "   - AOF files (KV data)"
echo "   - Etcd registration data"
echo "   - RabbitMQ messages"
echo "   - CDC logs"
echo ""
read -p "Continue? (yes/NO): " -r

if [[ $REPLY == "yes" ]]; then
    docker-compose down -v
    echo "✅ All volumes deleted"
else
    echo "❌ Cancelled"
fi
