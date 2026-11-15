#!/usr/bin/env bash
# Apply database migrations (up) or report status.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a; [ -f .env ] && source .env; set +a

case "${1:-up}" in
  up)
    echo "==> Applying migrations to ${DATABASE_URL%%\?*}"
    (cd services/api && go run ./cmd/migrate -dir ./migrations)
    ;;
  down)
    echo "Veylo migrations are forward-only by design."
    echo "To reset local data use: make reset-local"
    exit 1
    ;;
  *)
    echo "usage: $0 [up]" >&2
    exit 2
    ;;
esac
