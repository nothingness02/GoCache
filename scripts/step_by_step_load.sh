#!/bin/bash
echo "🔥 [Step 1] Starting High Concurrency Load Test..."
echo "Target: http://localhost:8080/api/v1/kv?key=test_pprof"
echo "Threads: 50"
echo "Duration: Indefinite (Ctrl+C to stop)"

# Spawn 50 background workers
for i in {1..50}; do
  (while true; do curl -s "http://localhost:8080/api/v1/kv?key=test_pprof" > /dev/null; done) &
done

echo "✅ Load generator is RUNNING! (CPU should be heating up now)"
echo "Info: Press 'pkill -P $$' to stop."

# Wait indefinitely
wait
