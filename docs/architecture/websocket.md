# WebSocket Protocol (v1)

Veylo's realtime layer uses a single authenticated WebSocket connection per
browser session carrying JSON envelopes.

## Endpoint & authentication

```text
POST /v1/ws/token            → { ticket, expires_in: 30 }   (session cookie required)
GET  wss://host/v1/ws?ticket=<ticket>                        (ticket consumed atomically)
```

Tickets are one-time (Redis `GETDEL`), expire after 30 seconds, and bind the
connection to the authenticated user + session. Client code lives in
`apps/web/lib/ws.ts`; server hub in `services/api/internal/realtime/hub.go`.

## Envelope

```json
{
  "v": 1,
  "type": "message.created",
  "event_id": "uuid",
  "ts": "2026-01-01T00:00:00.000Z",
  "data": { }
}
```

- `v` — protocol version (1).
- `event_id` — unique per event; clients keep an LRU cache (~600 ids) and drop
  duplicates, making redeliveries harmless.
- `data` — event payload (object).

## Client → server events

| Type             | Data                                                                 | Notes |
| ---------------- | -------------------------------------------------------------------- | ----- |
| `ping`           | `{}`                                                                 | App-level heartbeat; replies `pong` |
| `typing.start`   | `{ conversation_id }`                                                | Membership checked; ephemeral Redis TTL 6s |
| `typing.stop`    | `{ conversation_id }`                                                | |
| `receipt.deliver`| `{ conversation_id, max_seq }`                                       | Monotonic; updates cursors + per-message receipts |
| `receipt.read`   | `{ conversation_id, max_seq }`                                       | Monotonic; implies delivery |
| `message.send`   | `{ conversation_id, type, text, reply_to_id?, attachment_ids?, client_message_id }` | Persists transactionally; idempotent |

The web client sends read receipts over REST (`POST /v1/conversations/{id}/read`)
for determinism at page load; the WS event remains available for clients that
prefer socket-only traffic. Both paths share the same server logic and fanout.

## Server → client events

| Type                        | Data (summary) |
| --------------------------- | -------------- |
| `connection.ready`          | `{ user_id, conn_id, server_time, protocol, heartbeat_sec }` |
| `pong`                      | `{ client_event_id }` |
| `connection.error`          | `{ error, code?, in_response_to? }` |
| `message.sent`              | `{ client_message_id, message }` — ack to the originating connection |
| `message.created`           | `{ message }` — to all member sessions (except originating conn) |
| `message.updated`           | `{ message }` — edits |
| `message.deleted`           | `{ conversation_id, message_id }` |
| `message.failed`            | `{ client_message_id, error: { code, message } }` |
| `receipt.delivered`         | `{ conversation_id, user_id, max_seq }` |
| `receipt.read`              | `{ conversation_id, user_id, max_seq }` — suppressed if the reader disabled read receipts |
| `typing.start` / `typing.stop` | `{ conversation_id, user_id }` |
| `presence.online`           | `{ user_id }` — audience filtered by visibility settings + blocks |
| `presence.offline`          | `{ user_id, last_seen_at }` |
| `conversation.created`      | `{ conversation }` |
| `conversation.updated`      | `{ conversation }` |
| `conversation.deleted`      | `{ conversation_id }` |
| `conversation.member_added` | `{ conversation, added_user_ids }` |
| `conversation.member_removed` | `{ conversation_id, removed_user_id }` |
| `reaction.added` / `reaction.removed` | `{ message_id, conversation_id, user_id, emoji }` |
| `notification.created`      | `{ notification }` |

## Reliability rules

1. **Nothing is broadcast before it is committed.** `message.created` is only
   emitted after the PostgreSQL transaction commits.
2. **Sequence numbers are authoritative.** Clients order by `message.seq`;
   timestamps are display-only.
3. **Reconnection:** clients reconnect with exponential backoff + jitter, and
   re-sync over REST on `__resync` (internal event on socket open). Missed
   events are recovered from the database, not replayed from memory.
4. **Delivery semantics:** `receipt.deliver` = at least one recipient session
   got the event; `receipt.read` = the recipient advanced their cursor. Read
   receipts are suppressed for users who disabled them (privacy setting).
5. **Heartbeats:** server pings every 30s (gorilla control frames); client
   JSON `ping` every 25s; read deadline 75s.
6. **Slow consumers** are disconnected rather than blocking the hub; they
   recover through the REST re-sync path.

## Versioning

Additions to `data` payloads are backwards-compatible. Behavior changes bump
`v` and are announced in release notes.
