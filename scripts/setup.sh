#!/usr/bin/env bash
# Veylo one-time development setup.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Checking prerequisites"
command -v docker >/dev/null || { echo "Docker is required (docker compose)."; exit 1; }
command -v pnpm >/dev/null || { echo "pnpm is required: https://pnpm.io/installation"; exit 1; }
command -v go >/dev/null || { echo "Go is required: https://go.dev/dl/"; exit 1; }

echo "==> Creating .env from .env.example (if missing)"
if [ ! -f .env ]; then
  cp .env.example .env
  # Generate real local secrets.
  if command -v openssl >/dev/null; then
    KEY1=$(openssl rand -hex 32)
    KEY2=$(openssl rand -hex 32)
    sed -i '' \
      -e "s|replace-with-random-32-byte-hex|$KEY1|1" \
      .env 2>/dev/null || sed -i "s|replace-with-random-32-byte-hex|$KEY1|1" .env
    python3 - "$KEY2" <<'PY'
import sys, re
key2 = sys.argv[1]
text = open('.env').read()
text = text.replace('replace-with-random-32-byte-hex', key2, 1)
open('.env', 'w').write(text)
PY
  fi
  echo "    .env created with fresh local secrets."
else
  echo "    .env already exists — left untouched."
fi

echo "==> Installing Node dependencies"
pnpm install

echo "==> Tidying Go modules"
(cd services/api && go mod tidy)

echo "==> Starting infrastructure (postgres, redis, mailpit)"
docker compose up -d postgres redis mailpit

echo "==> Waiting for services"
./scripts/health-check.sh

echo "==> Applying migrations"
./scripts/migrate.sh up

echo
echo "Setup complete. Start the app with:  make dev"
