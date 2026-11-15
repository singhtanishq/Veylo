<div align="center">

# Veylo

### Conversations without boundaries.

**A private, real-time communication platform.** Independent people register,
discover one another, and exchange persistent messages instantly — with
delivery/read receipts, presence, typing indicators, groups, media sharing and
multi-device sync. Everything runs on your machine.

`Go` · `Next.js 16` · `TypeScript` · `PostgreSQL` · `Redis` · `WebSockets` · `Tailwind CSS 4`

[Quick start](#-quick-start) · [Screenshots](#-screenshots) · [Architecture](#-architecture) · [API](docs/api/README.md) · [Security](#-security-model)

</div>

---

## ✨ Features

**Accounts & identity**
- Registration with email verification (real tokens, delivered through the local Mailpit server)
- Login with email *or* username, Argon2id password hashing, password reset
- Server-side sessions with device labels, session list and per-session revocation
- Editable profiles: display name, unique `@username`, about text, avatar upload, optional phone number

**Discovery & contacts**
- Find people by `@username`, display name, email or (verified) phone — honoring each user's privacy settings
- Contact requests with mutual auto-accept, contact management, blocking

**Messaging**
- Direct conversations (duplicate-safe) and groups with owner/admin/member roles
- Persistent, server-ordered messages with idempotent sends (safe retries)
- Live delivery: typing indicators, presence, delivery and read receipts
- Replies, reactions, editing (within a configurable window), deletion, message search
- Cursor-paginated history that survives refresh, reconnection and restarts

**Media**
- Images, video, audio, voice notes and documents with upload progress
- Server-side content sniffing (client MIME is never trusted), size limits, image thumbnails
- Membership-authorized downloads and a per-conversation media gallery

**Real-time infrastructure**
- One authenticated WebSocket per browser session (one-time ticket auth, heartbeats)
- Event de-duplication, exponential-backoff reconnection, REST re-sync on recovery
- Multi-session sync: messages sent from one device appear on the others instantly

---

## 📸 Screenshots

Every image below is a **real render of the running application** captured with
Playwright (desktop 1440×1000, mobile 390×844) — no mockups. The full manifest
with per-image metadata lives in
[docs/screenshots/SCREENSHOT_INDEX.md](docs/screenshots/SCREENSHOT_INDEX.md).

### Authentication

#### Login

The sign-in screen with the Veylo brand panel and demo-account hints.

![Login](docs/screenshots/01-authentication/login.png)

#### Registration

Account creation with live username availability checking.

![Registration](docs/screenshots/01-authentication/registration.png)

#### Email verification

Verification instructions — the token arrives via the local Mailpit server.

![Email verification](docs/screenshots/01-authentication/email-verification.png)

#### Password recovery

The forgot-password flow, delivered locally with anti-enumeration semantics.

![Password recovery](docs/screenshots/01-authentication/password-reset.png)

#### Main workspace

The primary Veylo interface: navigation rail, conversation sidebar and active chat.

![Main workspace](docs/screenshots/02-main-interface/main-workspace.png)

#### Conversation sidebar

Filters, search, unread badges, pins and presence at a glance.

![Conversation sidebar](docs/screenshots/02-main-interface/conversation-sidebar.png)

#### Empty state

The workspace before a conversation is selected.

![Empty state](docs/screenshots/02-main-interface/empty-conversation.png)

#### Responsive layout

The same workspace adapting to a narrower viewport.

![Responsive layout](docs/screenshots/02-main-interface/responsive-layout.png)

#### Discovery

Find-people landing with query syntax hints.

![Discovery](docs/screenshots/03-user-discovery/discovery-search.png)

#### Username search

`@username` lookup with live presence indicators.

![Username search](docs/screenshots/03-user-discovery/username-results.png)

#### Email search

Privacy-respecting email discovery (opt-out honored).

![Email search](docs/screenshots/03-user-discovery/email-results.png)

#### Profile preview

Discovery card with contact/message actions.

![Profile preview](docs/screenshots/03-user-discovery/user-profile-preview.png)

#### Direct conversation

Persistent one-to-one chat with timestamps, read receipts (✓✓) and reactions.

![Direct conversation](docs/screenshots/04-conversations/direct-conversation.png)

#### Message history

Full durable history — nothing disappears on refresh.

![Message history](docs/screenshots/04-conversations/message-history.png)

#### Replies

Reply banner quoting the original message.

![Replies](docs/screenshots/04-conversations/reply-message.png)

#### Reactions

The reaction picker over any message.

![Reactions](docs/screenshots/04-conversations/reactions.png)

#### Context menu

Per-message actions: reply, copy, edit, delete.

![Context menu](docs/screenshots/04-conversations/message-context-menu.png)

#### Typing indicator

Realtime typing while the other person composes.

![Typing indicator](docs/screenshots/04-conversations/typing-indicator.png)

#### Group creation

Multi-select people, name the group, create it.

![Group creation](docs/screenshots/05-groups/group-creation.png)

#### Group conversation

Group chat with per-message sender names and system events.

![Group conversation](docs/screenshots/05-groups/group-conversation.png)

#### Group details

Description, member roster with roles and management actions.

![Group details](docs/screenshots/05-groups/group-details.png)

#### Group members

Owner/admin/member roles with add/remove controls.

![Group members](docs/screenshots/05-groups/group-members.png)

#### Image messages

Seeded photo attachment rendered inline.

![Image messages](docs/screenshots/06-media/image-message.png)

#### Document messages

Document card with size and download action.

![Document messages](docs/screenshots/06-media/document-message.png)

#### Attachment upload

Composer attachment chips with size info.

![Attachment upload](docs/screenshots/06-media/attachment-upload.png)

#### Media gallery

Per-conversation media grid with previews.

![Media gallery](docs/screenshots/06-media/media-gallery.png)

#### Contact list

Accepted contacts with presence.

![Contact list](docs/screenshots/07-contacts/contact-list.png)

#### Contact requests

Incoming request with the sender's message.

![Contact requests](docs/screenshots/07-contacts/contact-requests.png)

#### Blocked users

Block list with unblock actions.

![Blocked users](docs/screenshots/07-contacts/blocked-users.png)

#### Notification center

The notification feed with unread states and mark-as-read.

![Notification center](docs/screenshots/08-notifications/notification-center.png)

#### Unread notifications

The workspace bell showing unread activity.

![Unread notifications](docs/screenshots/08-notifications/unread-notifications.png)

#### My profile

Profile overview with avatar and verification state.

![My profile](docs/screenshots/09-profile/user-profile.png)

#### Profile editor

Editing display name, username and about text.

![Profile editor](docs/screenshots/09-profile/profile-editor.png)

#### Appearance

Light, dark and system theme selection.

![Appearance](docs/screenshots/10-settings/appearance.png)

#### Privacy

Visibility, read receipts and discoverability controls.

![Privacy](docs/screenshots/10-settings/privacy.png)

#### Security

Password change with strength policy.

![Security](docs/screenshots/10-settings/security.png)

#### Notification settings

Per-category notification toggles.

![Notification settings](docs/screenshots/10-settings/notifications.png)

#### Active sessions

Device sessions with last-active times and revocation.

![Active sessions](docs/screenshots/10-settings/active-sessions.png)

#### Account

Account details and sign-out actions.

![Account](docs/screenshots/10-settings/account.png)

#### Mobile conversation

The messaging experience on a 390×844 viewport.

![Mobile conversation](docs/screenshots/11-responsive/mobile-conversation.png)

#### Mobile discovery

Discovery on mobile.

![Mobile discovery](docs/screenshots/11-responsive/mobile-discovery.png)

#### Mobile settings

Settings on mobile.

![Mobile settings](docs/screenshots/11-responsive/mobile-settings.png)

#### Architecture overview

Rendered system architecture (from docs/architecture/overview.md).

![Architecture overview](docs/screenshots/12-system/architecture-overview.png)

#### Database architecture

Rendered PostgreSQL schema diagram (from docs/database/schema.md).

![Database architecture](docs/screenshots/12-system/database-architecture.png)

#### Local infrastructure

The Mailpit web UI capturing Veylo's local email.

![Local infrastructure](docs/screenshots/12-system/local-infrastructure.png)

---

## 🏗 Architecture

```text
┌──────────────────────────┐   REST + WebSocket   ┌────────────────┐
│  Next.js web (:3000)     │◄────────────────────►│  Go API (:8080)│
│  React 19 · TanStack Q   │  cookies + tickets   │  chi · pgx     │
│  Zustand (ephemeral)     │                      │  WS hub        │
└──────────────────────────┘                      └───┬────┬───┬──┘
                                      ┌───────────────┘    │   └─────────┐
                                      ▼                    ▼             ▼
                              ┌──────────────┐     ┌──────────┐   ┌─────────────┐
                              │ PostgreSQL   │     │ Redis    │   │ Local disk  │
                              │ source of    │     │ presence │   │ media store │
                              │ truth        │     │ typing   │   │ storage/    │
                              └──────────────┘     │ tickets  │   └─────────────┘
                                                   └──────────┘   Mailpit :8025
                                                                   (local email)
```

- **PostgreSQL is the single source of truth.** WebSockets only accelerate
  delivery; any client recovers fully over REST after refresh, offline periods
  or backend restarts.
- **Server-authoritative ordering.** Per-conversation sequences are allocated
  transactionally; simultaneous sends are serialized by the database.
- **Idempotent sends.** `client_message_id` + a unique index make retries safe.
- **Ephemeral state (presence, typing, tickets) lives in Redis only.**

Deep dives: [architecture overview](docs/architecture/overview.md) ·
[WebSocket protocol](docs/architecture/websocket.md) ·
[database schema](docs/database/schema.md) ·
[REST API](docs/api/README.md) ·
[security model](docs/security/model.md).

## 🧰 Technology stack

| Layer | Choices |
| ----- | ------- |
| Frontend | Next.js 16 (App Router), React 19, TypeScript, Tailwind CSS 4, Radix UI primitives, TanStack Query, Zustand, Framer Motion, Lucide icons, Playwright, Vitest |
| Backend | Go, chi router, Gorilla WebSocket, pgx (PostgreSQL), go-redis, Argon2id, structured `slog` logging |
| Infrastructure | Docker Compose — PostgreSQL 17, Redis 7, Mailpit; local filesystem media storage |
| Quality | Go integration tests (real PG + Redis), Vitest, Playwright E2E, gitleaks, Trivy, GitHub Actions CI |

## 🗃 Database design

~25 tables covering identity, sessions, the social graph, conversations
(duplicate-safe direct pairs + groups with roles), messages (sequence-ordered,
idempotent, soft-deletable, full-text indexed), receipt cursors + per-message
receipts, attachments metadata, notifications, preferences, privacy settings
and an audit trail. See [docs/database/schema.md](docs/database/schema.md).

## ⚡ WebSocket communication model

One authenticated connection per browser session. Versioned JSON envelopes
(`v: 1`) with `event_id` de-duplication. The server broadcasts `message.created`
**only after the PostgreSQL transaction commits**, fans receipts and presence
with privacy filtering, and clients re-sync over REST on reconnect. The full
event catalog: [docs/architecture/websocket.md](docs/architecture/websocket.md).

## 🔐 Authentication & privacy

Argon2id password hashing · HTTP-only session cookies (hashed server-side) ·
email verification required before login · single-use password-reset tokens ·
login/register rate limiting · anti-enumeration responses · membership checks
on every message, receipt, attachment and search query · content-sniffed,
membership-authorized downloads · privacy controls for presence, last-seen,
read receipts and discoverability · blocking enforced across messaging,
search and presence. Details: [docs/security/model.md](docs/security/model.md).

---

## 🚀 Quick start

Prerequisites: **Docker**, **Go 1.25+**, **Node 20+**, **pnpm 12+**.

```bash
# 1 — one-time setup: creates .env with fresh secrets, installs deps,
#     starts PostgreSQL + Redis + Mailpit, applies migrations
./scripts/setup.sh

# 2 — run the API and the web app together
make dev
```

| Service | URL |
| ------- | --- |
| Web app | http://localhost:3000 |
| API | http://localhost:8080 |
| Mailpit (email) | http://localhost:8025 |

### Docker startup (application containers)

The default `docker compose up -d` runs the **infrastructure** (postgres, redis,
mailpit). The API and web images build under the `full` profile:

```bash
docker compose --profile full up -d --build
```

### Migrations

Migrations apply automatically on API startup, or manually:

```bash
cd services/api && go run ./cmd/migrate -dir ./migrations
```

## 👥 Demo accounts

```bash
make seed     # idempotent; safe to re-run
```

| Username | Email | Password |
| -------- | ----- | -------- |
| `alice` | alice@example.test | `VeyloDemo!2026` |
| `bob` | bob@example.test | `VeyloDemo!2026` |
| `charlie` | charlie@example.test | `VeyloDemo!2026` |
| `david` | david@example.test | `VeyloDemo!2026` |

Seeded content: contacts, two direct conversations (with reactions, a reply and
read receipts), one unread message, the **Project Aurora** group with all four
members, notifications and a demo image attachment. Development-only password —
never use it outside your machine.

## 📮 How to: verify email (Mailpit)

1. Register at `/register`.
2. Open **http://localhost:8025** — Mailpit captured the verification email.
3. Click **Verify email** (or paste the token at `/verify-email`).
4. Sign in.

Password resets work the same way. No real email ever leaves your machine.

## 💬 How to: register, discover and chat

1. **Register** — display name, `@username`, email, password (8+ chars, mixed case, a digit).
2. **Verify** your email through Mailpit, then sign in.
3. **Discover** someone: click the compass → search `@username`, a name, an
   email, or a verified phone number.
4. **Message** them — the conversation opens instantly; messages deliver in
   real time with ✓ sent → ✓✓ delivered → ✓✓ read receipts.
5. **Create a group**: `+` → select several people → group name → Create.
   Owners/admins can add/remove members and promote admins.

## 🖼 How to: share media

Click the 📎 paperclip in any composer (or drag-paste via the file input),
pick images / video / audio / PDF / documents (≤ 25 MB), watch the upload
progress, then send. Recipients get inline previews, a per-conversation media
gallery (chat header → details → Media), and authorized downloads.

## 🧪 Testing

```bash
make test        # Go build+vet+tests, typecheck, lint, Vitest
make lint        # gofmt, vet, ESLint, gitleaks (if installed)

# Playwright E2E — the full multi-user journey (needs the stack running):
cd tests/e2e && pnpm exec playwright install chromium
pnpm exec playwright test
```

The E2E suite runs twelve steps across independent browser contexts: three
users register and verify through Mailpit, discover each other, exchange
real-time messages with receipts, react, share attachments, form a group with
member management, test offline recovery, multi-session sync, blocking, IDOR
denial and session revocation.

## 🔒 Security model

Short version: Argon2id, hashed server-side sessions, membership checks
everywhere, sniffed uploads, rate limits, CSRF origin checks, plain-text
rendering (no HTML injection surface), privacy-aware receipts/presence, audit
logging. **Messages are not end-to-end encrypted** and local traffic is plain
HTTP — Veylo never claims otherwise. Full model + deployment checklist:
[docs/security/model.md](docs/security/model.md) and
[SECURITY.md](SECURITY.md).

## 📚 API documentation

The complete REST surface (auth, users, contacts, conversations, messages,
receipts, attachments, search, notifications, sessions, settings) with request
shapes and error codes: [docs/api/README.md](docs/api/README.md).

## ⚠️ Known limitations

Honest list, by design:

- **No end-to-end encryption** — transport is plain HTTP on localhost; E2EE is
  a roadmap item, not a claim.
- **Phone numbers stay unverified** locally (no SMS provider); phone discovery
  only matches verified numbers, so it stays inactive.
- **Single-instance rate limits** (in-process token buckets) — Redis-backed
  limits are the documented multi-instance upgrade.
- **Observability** is structured logging + request IDs; OpenTelemetry export
  is an extension point, not faked.
- **OpenAPI spec** is hand-maintained from the code contracts; generation is a
  follow-up.

## 🗺 Roadmap

- End-to-end encryption (audited protocol; device identity + multi-device keys)
- Voice/video calls (WebRTC, local-only)
- Message scheduling, polls and read-state per group member
- OpenAPI generation + typed client publishing
- Optional OTel tracing export and Prometheus metrics

## 🛠 Troubleshooting

| Symptom | Fix |
| ------- | --- |
| `database: ping` at API start | `make infra-up` then `make health` |
| No verification email | Mailpit down → open :8025; restart the mailpit container |
| `429 Too many requests` | Rate limit reached → tune `RATE_*` in `.env`, restart the API |
| WS won't connect | Check :8080 reachability; tickets expire after 30 s — the client retries automatically |
| Port 3000/8080 busy | `lsof -nP -iTCP:3000 -sTCP:LISTEN` → kill the stale process |
| Media missing | `STORAGE_PATH` must match where uploads were written (`storage/uploads`) |
| Fresh start | `make reset-local` (interactive; wipes volumes + uploads) |

More: [docs/development/setup.md](docs/development/setup.md).

## 🤝 Contributing

Conventional Commits, tests for behavior changes, no secrets in commits, no
paid dependencies. See [CONTRIBUTING.md](CONTRIBUTING.md) and our
[Code of Conduct](CODE_OF_CONDUCT.md).

## 📄 License

[MIT](LICENSE) — © 2026 Tanishq Singh

## ✅ Screenshot validation report

Generated by [`scripts/validate-readme-screenshots.py`](scripts/validate-readme-screenshots.py)
(run `make validate-readme`):

```text
Approved screenshots on disk : 45
Embedded in README           : 45
Unreferenced screenshots     : 0
Broken image references      : 0
Manifest mismatches          : 0
```
