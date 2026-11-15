# Veylo REST API (v1)

Base URL: `http://localhost:8080/v1` — JSON, cookie-authenticated
(`veylo_session`, HTTP-only, SameSite=Lax).

## Response envelope

```json
{ "data": { … }, "request_id": "…" }
```

Errors:

```json
{ "error": { "code": "validation_error", "message": "…", "details": [ … ] }, "request_id": "…" }
```

Common codes: `unauthorized`, `forbidden`, `not_found`, `conflict`,
`email_exists`, `username_exists`, `email_not_verified`, `rate_limited`,
`validation_error`, `bad_request`, `internal_error`, `blocked`,
`payload_too_large`, `unsupported_media_type`.

All state-changing requests require same-origin (Origin header check) and
carry the `X-Requested-With` header from the web client. Rate limits apply
per-IP/per-user and are env-tunable (see below).

## Auth

| Method | Path | Body | Notes |
| ------ | ---- | ---- | ----- |
| POST | `/auth/register` | `{email, password, username, display_name}` | Creates `pending_verification` account; verification email via Mailpit; returns `{user_id, email, next, mailpit_url}` |
| POST | `/auth/verify-email` | `{token}` | Single-use token from email; activates account |
| POST | `/auth/resend-verification` | `{email?}` | Rate limited; anti-enumeration response |
| POST | `/auth/login` | `{identifier, password}` | Email **or** username; 403 `email_not_verified` until verified |
| POST | `/auth/logout` | — | Revokes current session |
| POST | `/auth/logout-all` | — | Revokes every session |
| GET  | `/auth/me` | — | Current user incl. email verification state |
| POST | `/auth/change-password` | `{current_password, new_password}` | |
| POST | `/auth/forgot-password` | `{email}` | Always `{sent: true}` (anti-enumeration); email via Mailpit |
| POST | `/auth/reset-password` | `{token, password}` | Single-use; revokes all sessions |

Rate overrides: `RATE_REGISTER_PER_HOUR` (default 20/IP), `RATE_LOGIN_PER_5MIN`
(default 30, per IP and per identifier).

## Users

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET | `/users/me` | Own profile incl. phone |
| PATCH | `/users/me` | `{display_name?, username?, about?}` |
| POST | `/users/me/phone` | `{phone}` E.164 or `""` to remove; stored unverified in the local build |
| GET | `/users/search?q=` | Interprets `@username`, email, E.164 phone, or fuzzy name; honors privacy settings; hides blockers |
| GET | `/users/username/availability?username=` | Public, IP rate-limited |
| GET | `/users/{id}` | Public profile (privacy-filtered presence) |
| POST | `/users/me/avatar` | multipart `file` (image) |

## Contacts

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET | `/contacts` | Accepted contacts |
| POST | `/contacts/requests` | `{user_id, message?}`; mutual request auto-accepts |
| GET | `/contacts/requests?direction=incoming\|outgoing` | |
| POST | `/contacts/requests/{id}/accept` / `decline` | |
| DELETE | `/contacts/{user_id}` | Removes contact (mutual) |
| GET/POST | `/contacts/blocks` | List / block `{user_id}` |
| DELETE | `/contacts/blocks/{user_id}` | Unblock |

## Conversations

| Method | Path | Notes |
| ------ | ---- | ----- |
| POST | `/conversations/direct` | `{user_id}` — get-or-create (duplicate-safe) |
| POST | `/conversations/group` | `{title, description?, member_ids[]}` |
| GET | `/conversations?filter=all\|unread\|groups\|direct\|archived\|pinned&q=` | List with unread counts, last message, presence |
| GET | `/conversations/{id}` | Details + members + roles |
| PATCH | `/conversations/{id}` | Group title/description (admin+) |
| DELETE | `/conversations/{id}` | Delete group (owner) |
| PATCH | `/conversations/{id}/settings` | `{is_muted?, is_pinned?, is_archived?, notification_level?}` (per-member) |
| POST | `/conversations/{id}/members` | `{user_ids[]}` (admin+ or if members_can_invite) |
| DELETE | `/conversations/{id}/members/me` | Leave (owner transfers to earliest member; last member deletes group) |
| DELETE | `/conversations/{id}/members/{user_id}` | Remove (admin+, not owners) |
| PATCH | `/conversations/{id}/members/{user_id}/role` | `{role: admin\|member}` (owner) |
| POST | `/conversations/{id}/invitations` | `{user_ids[]}` |
| GET | `/conversations/{id}/invitations` | Pending invites |
| POST | `/conversations/{id}/invitations/{invId}/accept` / `decline` | |
| POST | `/conversations/{id}/avatar` | multipart (admin+) |
| POST | `/conversations/{id}/read` | `{max_seq}` — advance read cursor (REST receipt path) |
| GET | `/conversations/{id}/gallery?kind=` | Shared media |

## Messages

| Method | Path | Notes |
| ------ | ---- | ----- |
| POST | `/conversations/{id}/messages` | `{type, text?, reply_to_id?, attachment_ids?, client_message_id}` — idempotent |
| GET | `/conversations/{id}/messages?limit=&before_seq=&after_seq=` | Cursor pagination (newest page first, `next_cursor`) |
| PATCH | `/messages/{id}` | `{text}` — own messages within the conversation edit window |
| DELETE | `/messages/{id}` | Soft delete (tombstone) |
| PUT/DELETE | `/messages/{id}/reactions` | `{emoji}` |
| GET | `/messages/{id}/receipts` | Per-recipient delivered/read detail (members only) |

## Attachments

| Method | Path | Notes |
| ------ | ---- | ----- |
| POST | `/attachments` | multipart `file`; content-sniffed (never trusts client MIME); ≤ `MAX_ATTACHMENT_SIZE_MB`; kind validated |
| GET | `/attachments/{id}/download` | Owner / avatar-reference / conversation-member authorization; inline for media, attachment otherwise; `nosniff` |
| GET | `/attachments/{id}/thumbnail` | Images (server-side resize) |
| DELETE | `/attachments/{id}` | Owner, if not attached to a message |

## Search

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET | `/search/messages?q=&conversation_id?=` | Full-text (`websearch` syntax); membership-scoped |
| GET | `/search/conversations?q=` | Title / participant name match |

## Notifications

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET | `/notifications` | Latest 50 |
| POST | `/notifications/read` | `{ids[]}` or `{all: true}` |

## Sessions & settings

| Method | Path | Notes |
| ------ | ---- | ----- |
| GET | `/sessions` | Active sessions with device info; `current` flagged |
| DELETE | `/sessions/{id}` | Revoke one |
| POST | `/sessions/revoke-others` | Revoke all other sessions |
| GET/PATCH | `/settings/privacy` | Visibility, read receipts, discoverability |
| GET/PATCH | `/settings/notifications` | Notification toggles, theme |
| POST | `/ws/token` | One-time WebSocket ticket (30s TTL) |

## Health

`GET /healthz` — liveness. `GET /readyz` — checks PostgreSQL + Redis.

OpenAPI: the TypeScript contracts in `apps/web/lib/types.ts` and the Go
handler DTOs are the source of truth; this document mirrors them. A generated
OpenAPI spec is a documented follow-up, not a claim.
