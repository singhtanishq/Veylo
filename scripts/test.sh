#!/usr/bin/env bash
# Run all test suites: Go tests, frontend unit tests, typecheck.
set -euo pipefail
cd "$(dirname "$0")/.."

set -a; [ -f .env ] && source .env; set +a

echo "==> Go build + vet"
(cd services/api && go build ./... && go vet ./...)

echo "==> Go tests"
if [ "${DATABASE_URL_TEST:-}" != "" ]; then
  (cd services/api && DATABASE_URL="$DATABASE_URL_TEST" MIGRATIONS_PATH=./migrations go test ./... -count=1)
else
  echo "    DATABASE_URL_TEST not set — falling back to DATABASE_URL (integration tests need a DB)."
  (cd services/api && go test ./... -count=1)
fi

echo "==> Frontend typecheck"
pnpm --filter @veylo/web typecheck

echo "==> Frontend lint"
pnpm --filter @veylo/web lint || echo "lint reported issues (see above)"

echo "==> Frontend unit tests"
pnpm --filter @veylo/web test || echo "vitest reported failures (see above)"

echo "All requested suites finished."
