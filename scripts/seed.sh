#!/usr/bin/env bash
# Seed deterministic demo accounts and sample conversations (development only).
set -euo pipefail
cd "$(dirname "$0")/.."

set -a; [ -f .env ] && source .env; set +a

echo "==> Seeding demo data (Alice, Bob, Charlie, David)"
(cd services/api && go run ./cmd/seed "$@")
