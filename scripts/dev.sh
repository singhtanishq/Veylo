#!/usr/bin/env bash
# Start the Go API and Next.js web app together for local development.
# Requires infrastructure (postgres/redis/mailpit) to be running: make infra-up
set -euo pipefail
cd "$(dirname "$0")/.."

if [ ! -f .env ]; then
  echo "Missing .env — run scripts/setup.sh first." >&2
  exit 1
fi

# Load .env (export all KEY=VALUE lines).
set -a
source .env
set +a

cleanup() {
  echo ""
  echo "==> Stopping services..."
  kill 0 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "==> Starting Go API on :${API_PORT:-8080}"
(cd services/api && go run ./cmd/api) &
API_PID=$!

echo "==> Starting Next.js web on :${WEB_PORT:-3000}"
(cd apps/web && pnpm dev) &
WEB_PID=$!

echo
echo "   Web:   http://localhost:${WEB_PORT:-3000}"
echo "   API:   http://localhost:${API_PORT:-8080}"
echo "   Mail:  http://localhost:8025  (Mailpit)"
echo
wait -n "$API_PID" "$WEB_PID"
