# ==========================================================================
# Veylo — developer commands
# ==========================================================================

# ---- Infrastructure (Docker Compose: postgres, redis, mailpit) ----
infra-up:
	docker compose up -d
	@echo "Waiting for services to become healthy..."
	./scripts/health-check.sh || true

infra-down:
	docker compose down

infra-logs:
	docker compose logs -f

# ---- Application ----
dev:
	./scripts/dev.sh

build:
	cd services/api && go build -o bin/api ./cmd/api
	pnpm --filter @veylo/web build

# ---- Database ----
migrate:
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

seed:
	./scripts/seed.sh

# ---- Quality ----
test:
	./scripts/test.sh

lint:
	./scripts/lint.sh

typecheck:
	pnpm --filter @veylo/web typecheck

health:
	./scripts/health-check.sh

# ---- Utilities ----
setup:
	./scripts/setup.sh

screenshots:
	./scripts/capture-screenshots.sh

validate-readme:
	python3 scripts/validate-readme-screenshots.py

# ---- Disposable local data reset (interactive safeguard) ----
reset-local:
	./scripts/reset-local.sh

.PHONY: infra-up infra-down infra-logs dev build migrate migrate-down seed test lint typecheck health setup screenshots validate-readme reset-local
