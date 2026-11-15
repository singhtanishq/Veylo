#!/usr/bin/env bash
# Run linters: Go vet/gofmt, ESLint, secret scan (if gitleaks installed).
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> gofmt check"
FMT=$(cd services/api && gofmt -l .)
if [ -n "$FMT" ]; then
  echo "These files need gofmt:"
  echo "$FMT"
  exit 1
fi

echo "==> go vet"
(cd services/api && go vet ./...)

echo "==> ESLint"
pnpm --filter @veylo/web lint

echo "==> gitleaks (if available)"
if command -v gitleaks >/dev/null; then
  gitleaks detect --redact --exit-code 1
  echo "    no secrets detected"
else
  echo "    gitleaks not installed — skipped locally (runs in CI)."
fi

echo "Lint checks passed."
