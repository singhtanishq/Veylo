# Database Schema

PostgreSQL is Veylo's single source of truth. Schema lives in
`services/api/migrations/*.sql` (applied in order, tracked in
`schema_migrations` with checksums; each migration runs in a transaction).

## Entity map

```text
users ──┬── user_profiles            (display name, about, avatar)
        ├── user_phone_numbers       (optional, verified flag)
        ├── email_verification_tokens / password_reset_tokens
        ├── sessions                 (server-side, hashed tokens)
        ├── user_preferences         (notification prefs, theme)
        ├── user_privacy_settings    (visibility, discovery, receipts)
        ├── contacts / contact_requests / blocked_users
        ├── notifications
        ├── attachments              (owned)
        └── conversation_members ──► conversations
                                          ├── direct_conversation_keys (pair key)
                                          ├── conversation_sequences   (seq counter)
                                          ├── conversation_settings    (edit window…)
                                          ├── group_invitations
                                          ├── messages ──┬── message_edits
                                                         ├── message_reactions
                                                         ├── message_attachments ──► attachments
                                                         └── message_receipts
                                          └── message_read_cursors
audit_logs                                 (security events, metadata only)
```

## Tables

### Identity

| Table | Purpose | Notable constraints |
| ----- | ------- | ------------------- |
| `users` | Core identity | `username` unique + regex `^[a-z0-9_]{3,24}$`; `email` unique, stored lowercase; `account_status` enum; Argon2id `password_hash` |
| `user_profiles` | Display profile | display name 1-80 chars, about ≤ 280, avatar reference |
| `user_phone_numbers` | Optional phone | E.164 format; **partial unique index on verified numbers only** |
| `email_verification_tokens` | Single-use tokens | SHA-256 hash stored, TTL, `used_at` |
| `password_reset_tokens` | Single-use reset | Same pattern; reset revokes all sessions |

### Sessions

`sessions` — one row per authenticated device. Stores **SHA-256 of the opaque
token** (raw tokens live only in the HTTP-only cookie), device label/type,
UA, IP, `last_active_at` (sliding renewal), `expires_at`, `revoked_at`.

### Social graph

| Table | Purpose |
| ----- | ------- |
| `contacts` | Accepted contacts (mirrored rows both directions) |
| `contact_requests` | Pending/accepted/declined requests, unique per pair |
| `blocked_users` | Blocker → blocked; blocks messaging, search visibility, presence |

### Conversations

| Table | Purpose |
| ----- | ------- |
| `conversations` | `direct` or `group`; title/description/avatar (groups); `last_message_at` |
| `conversation_members` | Membership + role (`owner`/`admin`/`member`), mute/pin/archive, notification level, `left_at` soft-leave; unique (conversation, user) |
| `direct_conversation_keys` | Canonical `pairKey` (`uuidA:uuidB`, sorted) — guarantees a single DM per unordered pair; creation serialized by `pg_advisory_xact_lock(hashtext(pair))` |
| `conversation_sequences` | `last_seq` counter; allocation via `INSERT … ON CONFLICT DO UPDATE … RETURNING` inside the send transaction |
| `conversation_settings` | Per-conversation policy (edit window seconds, members_can_invite) |
| `group_invitations` | Pending group invites with lifecycle |

### Messages

| Table | Purpose | Notes |
| ----- | ------- | ----- |
| `messages` | Core message | unique (conversation, seq); `client_message_id` unique per (conversation, sender) → idempotent sends; text ≤ 20 KB enforced by CHECK; system messages have NULL sender |
| `message_edits` | Edit history | previous/new text per edit |
| `message_reactions` | Emoji reactions | unique (message, user, emoji) |
| `message_attachments` | Message ↔ attachment | ordered `position` |

### Receipts

| Table | Purpose |
| ----- | ------- |
| `message_read_cursors` | Per (conversation, user): `delivered_seq`, `read_seq` — authoritative for unread counts and receipt checks |
| `message_receipts` | Per (message, user): `delivered_at`, `read_at` timestamps for the UI |

Reading = advancing `read_seq`; this implies delivery. Unread count =
messages with `seq > read_seq` from others. Receipts are written lazily for
the covered range when cursors advance (bounded, idempotent upserts).

### Media

`attachments` — metadata only; binaries live on disk under
`storage/uploads/{year}/{month}/{uuid}/{sanitized-name}`. Kind buckets
(image/video/audio/voice/document) drive rendering; dimensions are captured
for images; SHA-256 recorded.

### Settings & audit

- `notifications` — in-app notification center rows with read state.
- `user_preferences` — notification toggles, theme.
- `user_privacy_settings` — last-seen/online visibility (`everyone`/`contacts`/`nobody`), read receipts enabled, email/phone discoverability, who can send requests.
- `audit_logs` — security events (register/login/logout/reset/revoke/block) with IP, UA, JSON metadata. Never stores passwords, tokens or message content.

## Indexes (highlights)

```sql
idx_users_username_trgm        gin trigram  — username search
idx_users_email_trgm           gin trigram  — email search
idx_messages_conv_seq          (conversation_id, seq DESC) — history + pagination
idx_messages_client_id         partial unique — idempotent sends
idx_messages_fts               gin to_tsvector(simple, text) WHERE deleted IS NULL — full-text search
idx_members_user / idx_members_conv          — membership lookups
idx_conversations_last_message (last_message_at DESC)     — list ordering
idx_phone_verified_unique      partial unique on verified numbers
idx_notifications_user_unread  partial on unread
idx_sessions_user_active / idx_sessions_token_lookup        — auth fast path
```

## Concurrency notes

- **Duplicate DM prevention:** advisory lock on the pair key + unique index.
- **Sequence allocation:** single-row `UPDATE … RETURNING` serializes senders
  per conversation; safe under simultaneous sends.
- **Cursor advances:** monotonic `GREATEST(...)` upserts; races converge.

## Migrations

```bash
cd services/api
go run ./cmd/migrate -dir ./migrations   # applies pending, verifies checksums
```

Forward-only by design; `make reset-local` rebuilds disposable local data.
