# Veylo

**Conversations without boundaries.**

Veylo is a real-time communication platform — private, fast, and locally
self-contained. Independent users register, discover one another, and exchange
persistent messages in real time with delivery/read receipts, presence, typing
indicators, groups, media sharing and multi-device session sync.

> Status: **in active development** — this README grows as milestones land.

## Architecture at a glance

```text
┌─────────────┐   REST + WebSocket    ┌──────────────┐
│  Next.js    │ ◄──────────────────►  │   Go API     │
│  (apps/web) │   HTTP :3000→:8080    │  (services/  │
└─────────────┘                       │   api)       │
                                      └───┬────┬─────┘
                            ┌─────────────┘    │
                            ▼                  ▼
                     ┌────────────┐     ┌──────────┐
                     │ PostgreSQL │     │  Redis   │
                     │ (truth)    │     │ (ephem.) │
                     └────────────┘     └──────────┘
                            │
                            ▼
                     ┌────────────┐     ┌──────────┐
                     │ Local FS   │     │ Mailpit  │
                     │ (uploads)  │     │ (email)  │
                     └────────────┘     └──────────┘
```

- **Frontend** — Next.js (App Router), TypeScript, Tailwind CSS, Radix UI,
  TanStack Query, Zustand, Framer Motion.
- **Backend** — Go + chi, Gorilla WebSocket, pgx (PostgreSQL), go-redis.
- **Storage** — PostgreSQL is the single source of truth; Redis holds only
  ephemeral state (presence, typing, WS tickets).
- **Media** — local filesystem storage behind a swappable interface.
- **Email** — Mailpit captures verification / password-reset emails locally.

## Repository layout

```text
apps/web          Next.js application
services/api      Go backend (cmd/ + internal/ + migrations/)
docs              architecture, database, security and screenshot docs
scripts           developer utilities (setup, dev, test, migrate, seed…)
infra/docker      container builds
storage           local upload storage (gitignored)
```

## Quick start (local development, ₹0 / $0)

Requirements: Docker, Go 1.25+, Node 20+, pnpm.

```bash
# 1. One-time setup (creates .env, installs deps, starts infra, migrates)
./scripts/setup.sh

# 2. Start API + web
make dev
#    Web:  http://localhost:3000
#    API:  http://localhost:8080
#    Mail: http://localhost:8025 (Mailpit)
```

Register an account, grab the verification link from Mailpit, verify, log in —
then find a friend by `@username` or email and start chatting in real time.

## Demo data

```bash
make seed
```

Seeds four verified accounts (development-only password: `VeyloDemo!2026`):
`alice`, `bob`, `charlie`, `david` (all `@example.test`), plus conversations,
a group, reactions, replies, receipts and notifications.

## Tests

```bash
make test    # Go tests (needs local postgres), typecheck, lint, unit tests
```

## Security model (short version)

Argon2id password hashing · server-side sessions with HTTP-only cookies ·
membership-authorized messaging and attachments · rate limiting · CSRF origin
checks · server-side file validation. **Not** end-to-end encrypted (see
[SECURITY.md](SECURITY.md) and docs/security).

## License

[MIT](LICENSE)
