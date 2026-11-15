# Contributing to Veylo

Thank you for considering contributing to Veylo! 🎉

## Development setup

Follow the README "Local development" section:

1. Clone the repository and install dependencies (`pnpm install`).
2. Start local infrastructure: `make infra-up` (PostgreSQL, Redis, Mailpit).
3. Apply migrations: `make migrate`.
4. Start the API and web app: `make dev`.

## Ground rules

- **Conventional Commits**: `feat(auth): ...`, `fix(realtime): ...`, `docs: ...`.
- **Never commit secrets**: no `.env`, tokens, keys, or real user data. The
  repository is public — audit your staged files before committing.
- **Never commit `VEYLO.md`** (private project directive; already gitignored).
- **Never commit local media** (`storage/uploads/**` is gitignored).
- **Tests**: backend changes need `go test ./...` passing; frontend changes
  need `pnpm typecheck`, `pnpm lint` and `pnpm test:web` passing.
- **No paid dependencies**: Veylo must remain runnable locally at zero cost.
- Do not claim end-to-end encryption or features that are not implemented.

## Pull requests

1. Fork / branch from `main`.
2. Keep changes focused; one feature or fix per PR.
3. Describe what changed, how to test it, and any limitations.
4. Ensure CI checks pass (lint, typecheck, Go tests, secret scan).
