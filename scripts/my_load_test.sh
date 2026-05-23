#!/bin/bash
echo "Starting load on http://localhost:8080/api/v1/kv?key=test_pprof with 20 concurrent threads..."
for i in {1..20}; do
  (while true; do curl -s "http://localhost:8080/api/v1/kv?key=test_pprof" > /dev/null; done) &
done

echo "Traffic generated. Waiting..."
sleep 45
echo "Stopping load..."
pkill -P $$
exit 0
