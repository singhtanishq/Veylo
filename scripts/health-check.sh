#!/usr/bin/env bash
# Verify local infrastructure health: postgres, redis, mailpit.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0

wait_http() {
  local url="$1" name="$2" tries="${3:-30}"
  for i in $(seq 1 "$tries"); do
    if curl -sf -o /dev/null "$url"; then
      echo "  OK    $name ($url)"
      return 0
    fi
    sleep 1
  done
  echo "  FAIL  $name ($url) not responding"
  return 1
}

wait_tcp() {
  local host="$1" port="$2" name="$3" tries="${4:-30}"
  for i in $(seq 1 "$tries"); do
    if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
      exec 3>&- 3<&- || true
      echo "  OK    $name ($host:$port)"
      return 0
    fi
    sleep 1
  done
  echo "  FAIL  $name ($host:$port) not reachable"
  return 1
}

echo "==> Veylo local infrastructure health"
wait_tcp localhost 5432 "PostgreSQL" || fail=1
wait_tcp localhost 6379 "Redis" || fail=1
wait_tcp localhost 1025 "Mailpit SMTP" || fail=1
wait_http http://localhost:8025 "Mailpit Web UI" || fail=1

if [ "$fail" -ne 0 ]; then
  echo "Some services are unhealthy. Try: docker compose up -d"
  exit 1
fi
echo "All infrastructure healthy."
