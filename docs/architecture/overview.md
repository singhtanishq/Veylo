# Veylo Architecture Overview

Veylo is a real-time communication platform designed to run entirely on one
machine with zero paid services. This document describes the system shape,
the data flow and the key engineering decisions.

## System diagram

```text
┌────────────────────────────┐          ┌──────────────────────────────┐
│        Browser             │          │        Go API (:8080)        │
│  Next.js app (:3000)       │          │                              │
│  ┌──────────────────────┐  │  REST    │  ┌────────────────────────┐  │
│  │ React 19 + RSC shell │◄─┼─────────►│  │ chi router             │  │
│  │ TanStack Query       │  │ (cookies)│  │ auth middleware        │  │
│  │ Zustand (ephemeral)  │  │          │  │ domain handlers        │  │
│  └──────────────────────┘  │          │  └───────────┬────────────┘  │
│  ┌──────────────────────┐  │  WS      │              │               │
│  │ realtime client      │◄─┼──────────┼──► WebSocket hub             │
│  │ 1 conn / session     │  │ (ticket) │   per-user fanout            │
│  └──────────────────────┘  │          │                              │
└────────────────────────────┘          └───┬──────────┬─────────┬─────┘
                                            │          │         │
                            ┌───────────────┘          │         └──────────┐
                            ▼                          ▼                    ▼
                   ┌────────────────┐         ┌────────────┐      ┌──────────────┐
                   │ PostgreSQL     │         │ Redis      │      │ Local disk   │
                   │ source of truth│         │ ephemeral  │      │ storage/     │
                   │ (pgx pool)     │         │ state only │      │ uploads      │
                   └────────────────┘         └────────────┘      └──────────────┘
                                                       Mailpit (:8025)
                                                       local SMTP (:1025)
```

## Core principles

1. **PostgreSQL is the single source of truth.** Every message, membership,
   receipt cursor and session is persisted transactionally. WebSockets only
   *accelerate* delivery; any client can recover fully over REST.
2. **Server-authoritative ordering.** Each conversation has a `seq` counter
   allocated with `INSERT … ON CONFLICT DO UPDATE … RETURNING` inside the
   message's transaction. Simultaneous sends are serialized by the database,
   never by client clocks.
3. **Idempotent sends.** Every send carries a `client_message_id`; a
   `UNIQUE(conversation_id, sender_id, client_message_id)` index makes retries
   resolve to the original row instead of duplicating it.
4. **Ephemeral state lives in Redis only.** Presence (online set), typing
   indicators (TTL keys) and one-time WebSocket tickets never touch
   PostgreSQL. Losing Redis degrades presence briefly; nothing is lost.
5. **Authorization at every boundary.** Conversation membership is checked
   before any message, receipt, attachment or search access (IDOR protection).
   Attachments authorize via owner / avatar-reference / conversation-membership.
6. **Zero-cost local operation.** PostgreSQL, Redis, Mailpit and filesystem
   storage all run from one `docker compose up -d`.

## Domain packages (services/api/internal)

| Package         | Responsibility                                                              |
| --------------- | --------------------------------------------------------------------------- |
| `auth`          | Registration, Argon2id hashing, server-side sessions, email verification, password reset, session management |
| `users`         | Profiles, username availability, privacy-filtered discovery                 |
| `contacts`      | Contact requests (mutual auto-accept), contacts, blocking                   |
| `conversations` | Duplicate-safe direct conversations, groups, roles, invitations, member settings, system messages |
| `messages`      | Persistent messaging: sequence allocation, idempotent sends, pagination, edits, deletes, reactions |
| `receipts`      | Delivery/read cursors + per-message receipt rows, privacy-aware fanout      |
| `realtime`      | WebSocket hub: ticket auth, heartbeats, per-user fanout, event protocol     |
| `presence`      | Redis-backed online state + typing indicators                               |
| `attachments`   | Uploads with content sniffing, membership-authorized downloads, thumbnails, avatars |
| `search`        | Full-text message search, membership-scoped                                 |
| `notifications` | In-app notification center with preference filtering                        |
| `settings`      | Privacy + notification preferences                                          |
| `audit`         | Security-event audit trail (no secrets, no message content)                 |
| `platform/*`    | Config, database + migrations, Redis, mailer, storage, rate limiting        |

## Message send sequence

```text
Client (REST POST or WS message.send)
  │
  ▼
Authenticate (session cookie) ──► rate limit (60 msg/min per user)
  │
  ▼
Validate membership + content + attachments (inside one transaction)
  │
  ▼
Allocate seq (conversation_sequences, atomic UPDATE … RETURNING)
  │
  ▼
Insert message (idempotent via client_message_id) + wire attachments
  │
  ▼
Advance sender's cursors, touch conversation.last_message_at
  │
  ▼
COMMIT (PostgreSQL is now the source of truth)
  │
  ▼
message.created fanout to every member session (except originating conn)
sender's originating conn gets message.sent ack
notification rows written per recipient (respecting prefs + mentions)
  │
  ▼
Recipient clients ack: receipt.deliver (WS, on receipt of event)
Recipient clients ack: receipt.read  (REST, when conversation open + visible)
  │
  ▼
receipt.delivered / receipt.read events fan back to members
```

On reconnect, clients re-sync over REST (conversation list + messages by
`seq` cursor) and replay any missed events from the database — WebSocket
delivery is an optimization, never a requirement.

## WebSockets

One connection per authenticated browser session, authenticated with a
one-time 30-second ticket (`POST /v1/ws/token` → `GET /v1/ws?ticket=…`).
See [websocket.md](websocket.md) for the full protocol.

## Key decisions & documented trade-offs

- **Transactional outbox:** not required for the single-process local build —
  persist-then-fanout in-process plus REST re-sync gives identical guarantees
  for one instance. The send path is structured so an outbox can be inserted
  before the fanout step when multi-instance deployment needs it.
- **In-process rate limiting:** token buckets keyed per IP/user. Redis-backed
  limits are the upgrade path for multi-instance deployments.
- **OpenTelemetry:** structured `slog` logging with request IDs ships now;
  OTel export is a documented extension point, deliberately not faked.
- **E2E encryption:** explicitly *not* implemented and never claimed — see
  [security model](../security/model.md) and the roadmap in the README.

## Frontend architecture (apps/web)

- **Next.js App Router** with a server-side auth guard in the `(app)` layout
  (session cookie forwarded to the API; unauthenticated users never see the
  app shell).
- **TanStack Query** owns all server state (conversations, messages,
  receipts); WebSocket events patch/invalidate the same caches, so there is a
  single source of client truth.
- **Zustand** holds only ephemeral UI state: connection status, presence map,
  typing indicators, optimistic outgoing messages.
- **Realtime client** (`lib/ws.ts`): singleton per session, ticket auth,
  exponential backoff with jitter, `event_id` de-duplication cache, heartbeat
  pings, REST re-sync trigger on reconnect, disposal on logout.
- **Optimistic sends:** `client_message_id` generated locally → pending
  bubble rendered → replaced when persisted → visible failure + retry on
  error. Duplicates are impossible by construction (unique index server-side).
