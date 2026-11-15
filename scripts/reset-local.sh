#!/usr/bin/env bash
# Reset disposable local development data (database, redis, uploads, mail).
# DESTRUCTIVE — requires explicit confirmation. Never run automatically.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "This will permanently remove:"
echo "  - Docker volumes: postgres-data, redis-data, mailpit-data"
echo "  - Local uploads under storage/uploads"
echo
read -r -p "Type 'reset' to confirm: " answer
if [ "$answer" != "reset" ]; then
  echo "Aborted."
  exit 1
fi

echo "==> Stopping containers and deleting volumes"
docker compose down -v

echo "==> Removing local uploads"
rm -rf storage/uploads
mkdir -p storage/uploads
touch storage/.gitkeep

echo "==> Restarting infrastructure"
docker compose up -d postgres redis mailpit
./scripts/health-check.sh

echo "==> Re-applying migrations"
./scripts/migrate.sh up

echo "Local environment reset. Optionally seed demo data: make seed"
