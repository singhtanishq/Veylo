# Local Development Guide

Everything Veylo needs runs locally and costs nothing. Prerequisites:
**Docker**, **Go 1.25+**, **Node 20+**, **pnpm 12+**.

## First-time setup

```bash
./scripts/setup.sh        # .env + deps + infra + migrations
make dev                  # API :8080 + Web :3000 (foreground)
```

Manual equivalent:

```bash
cp .env.example .env                      # then generate secrets:
openssl rand -hex 32                      # → APP_ENCRYPTION_KEY
openssl rand -hex 32                      # → SESSION_SECRET
pnpm install
docker compose up -d postgres redis mailpit
(cd services/api && go run ./cmd/migrate -dir ./migrations)
make dev
```

## Daily commands

| Command | What it does |
| ------- | ------------ |
| `make infra-up` / `make infra-down` | Start/stop PostgreSQL + Redis + Mailpit |
| `make dev` | Run Go API + Next.js dev server together |
| `make migrate` | Apply pending migrations |
| `make seed` | Seed demo accounts + conversations |
| `make test` | Go build/vet/tests + typecheck + lint + vitest |
| `make lint` | gofmt, go vet, ESLint, gitleaks (if installed) |
| `make health` | Check infra health (ports + Mailpit UI) |
| `make reset-local` | **Interactive** wipe of volumes + uploads (never automatic) |
| `make screenshots` | Capture the full screenshot set (after seeding) |
| `make validate-readme` | Validate README screenshot references |

## URLs

| Service | URL |
| ------- | --- |
| Web app | http://localhost:3000 |
| API | http://localhost:8080 |
| Mailpit (email) | http://localhost:8025 |
| Mailpit SMTP | localhost:1025 |
| PostgreSQL | localhost:5432 (`veylo` / `veylo_local_dev`) |
| Redis | localhost:6379 |

## Demo accounts (after `make seed`)

| Username | Email | Password |
| -------- | ----- | -------- |
| `alice` | alice@example.test | `VeyloDemo!2026` |
| `bob` | bob@example.test | `VeyloDemo!2026` |
| `charlie` | charlie@example.test | `VeyloDemo!2026` |
| `david` | david@example.test | `VeyloDemo!2026` |

Seeded data: contacts between the four, DM conversations with reactions,
replies and read receipts, one unread message for Alice, a "Project Aurora"
group with all four members, notifications, and a demo image attachment.

Registration of completely independent accounts works without seeding.

## Multi-user testing

The E2E suite (`tests/e2e`) exercises everything automatically with
independent browser contexts:

```bash
cd tests/e2e
pnpm exec playwright install chromium   # once
pnpm exec playwright test               # requires API+Web+infra running
```

For manual two-user testing, use two browser profiles/incognito windows —
cookies are shared within one profile, so one window per user.

Email verification tokens: open Mailpit (http://localhost:8025) and click the
link in the message.

## Environment variables

Copy `.env.example` → `.env`. Highlights:

| Variable | Default | Purpose |
| -------- | ------- | ------- |
| `DATABASE_URL` | veylo@localhost:5432/veylo | PostgreSQL DSN |
| `DATABASE_URL_TEST` | …/veylo_test | Integration test DB (`make test`) |
| `REDIS_URL` | localhost:6379/0 | Ephemeral state |
| `APP_ENCRYPTION_KEY` / `SESSION_SECRET` | — | Local secrets (`openssl rand -hex 32`) |
| `SMTP_HOST/PORT` | localhost:1025 | Mailpit |
| `STORAGE_PATH` | ./storage/uploads | Media storage |
| `MAX_ATTACHMENT_SIZE_MB` | 25 | Upload limit |
| `RATE_REGISTER_PER_HOUR` | 20 | Registration rate per IP |
| `RATE_LOGIN_PER_5MIN` | 30 | Login rate per IP / identifier |

## Migrations

Forward-only SQL files in `services/api/migrations/`, checksum-tracked:

```bash
cd services/api && go run ./cmd/migrate -dir ./migrations
```

The API applies pending migrations automatically on startup too.

## Troubleshooting

| Symptom | Cause → fix |
| ------- | ----------- |
| `database: ping` on API start | Postgres not up → `make infra-up`, then `make health` |
| Verification email missing | Mailpit down → check :8025; restart `docker compose up -d mailpit` (or the native binary) |
| `429` on login/register | Rate limit hit → see `RATE_*` envs; restart API to reset in-memory buckets |
| WebSocket won't connect | `veylo:ws:ticket` expired (30s) — the client retries automatically; check :8080 reachability |
| Port 3000/8080 busy | `lsof -nP -iTCP:3000 -sTCP:LISTEN` → kill stale process |
| Media not rendering | `STORAGE_PATH` mismatch between API runs; files live under `storage/uploads` |
| Full local reset | `make reset-local` (wipes volumes + uploads; interactive confirm) |

## Test databases

Integration tests use `veylo_test` (auto-created in CI). Locally:

```bash
docker exec veylo-postgres psql -U veylo -d postgres \
  -c "CREATE DATABASE veylo_test;"
cd services/api && DATABASE_URL="postgres://veylo:veylo_local_dev@localhost:5432/veylo_test?sslmode=disable" \
  go test ./tests/ -count=1
```
