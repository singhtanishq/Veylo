# Veylo Security Model

Veylo targets **local development environments**. This document states
precisely what is protected, how, and what is explicitly out of scope.

## Implemented protections

### Account security
- **Argon2id** password hashing (m=64 MiB, t=2, p=2, 16-byte salt), OWASP-
  aligned parameters. Plaintext passwords never touch the database or logs.
- **Server-side sessions**: 32-byte opaque tokens in HTTP-only, SameSite=Lax
  cookies; only SHA-256 hashes stored; sliding renewal; expiry; per-session
  revocation; "sign out everywhere" on password reset.
- **Email verification required** before login (token single-use, hashed,
  expiring). No bypass exists that marks accounts verified without a valid
  token.
- **Password reset**: single-use hashed tokens, 1-hour TTL, revokes all
  sessions on success.
- **Brute-force controls**: login rate limited per IP *and* per identifier;
  registration per IP; verification resend; password reset. Policies are
  env-tunable and documented (`RATE_*`).
- **Account enumeration damping**: identical generic errors for unknown
  identifiers vs. wrong passwords; `forgot-password` always answers the same.

### Authorization
- Every conversation-scoped resource (messages, receipts, gallery, settings)
  checks **conversation membership** server-side — IDOR protection is central,
  not bolted on. Deep-linking an unauthorized conversation yields an explicit
  "unavailable" state and leaks nothing.
- Attachment downloads authorize via owner, avatar-reference, or
  conversation-membership.
- Group role enforcement (owner/admin/member) server-side; owners cannot be
  removed by admins; ownership transfers on leave.

### Messaging & input handling
- Message text length enforced server-side (20 KB CHECK + 8k-char app limit).
- User content renders as plain text (React default escaping) — **no**
  `dangerouslySetInnerHTML` anywhere; no HTML injection path by design.
- Search input goes through parameterized `websearch_to_tsquery` queries.

### File security
- Uploads validated by **content sniffing** (`http.DetectContentType`); the
  client-supplied MIME is never trusted; kind allow-lists.
- Size limits enforced twice (`MaxBytesReader` + actual read).
- Filenames sanitized; storage keys are server-generated UUID paths;
  traversal-safe path resolution.
- Downloads served with `Content-Disposition` (inline only for safe media),
  `X-Content-Type-Options: nosniff`, private cache headers; uploaded files are
  never executed.

### CSRF & origins
- Cookies are SameSite=Lax **plus** an Origin allow-list check on unsafe
  methods, plus CORS restricted to the configured frontend origins with
  credentials.

### Privacy
- Presence (online / last-seen) visibility: everyone / contacts / nobody —
  enforced in profile projections, conversation member lists, and presence
  event fanout.
- Read receipts can be disabled per user; suppression applies to events and
  receipt projections.
- Email discovery opt-out; phone discovery only for **verified** numbers
  (which the local build never fabricates — there is no SMS provider).
- Blocking hides users from search (both directions), stops messaging, and
  removes presence.
- Search results never include email/phone values.

### Rate limiting
Token buckets per key (IP / user / identifier) on register, login, verify,
reset, search, messaging, typing and uploads. In-process by design (single
local instance); Redis-backed is the documented multi-instance upgrade.

### Logging & audit
- Logs never contain passwords, tokens, cookies or message content.
- `audit_logs` records security events (register, login, failures, logout,
  verification, resets, revocations, blocks) with IP/UA/metadata only.
- Internal server errors log with request IDs; clients get generic messages.

## Explicitly NOT implemented (honest limitations)

1. **End-to-end encryption.** Messages are plaintext between the browser and
   the local API. Transport on localhost is plain HTTP. E2EE (Double Ratchet /
   MLS with device identity, key verification, multi-device sync and group key
   management) is a separate future milestone and is **never claimed** as done.
2. **HTTPS/TLS.** Local development runs on plain HTTP. Non-local deployment
   requires TLS termination and the `Secure` cookie flag (already conditional
   on `APP_ENV`).
3. **SMS/phone verification.** No paid SMS dependency; numbers are stored
   unverified and are invisible to discovery until a real provider integration
   exists.
4. **OpenAPI generation.** API docs are hand-maintained from the code
   contracts; generated specs are a follow-up.
5. **OpenTelemetry export.** Structured logging + request IDs ship; trace
   export is a documented extension point.
6. **Multi-instance hardening.** Rate limits and fanout are in-process;
   horizontal scaling needs Redis-backed limits and (optionally) a
   transactional outbox. The code paths are structured for this.

## Deployment checklist (non-local)

1. Generate real `APP_ENCRYPTION_KEY` / `SESSION_SECRET` (`openssl rand -hex 32`).
2. Terminate TLS; enable secure cookies; restrict CORS origins to your domain.
3. Replace Mailpit with authenticated SMTP.
4. Review all `RATE_*` values for your user base.
5. Store `storage/uploads` on a persistent, backed-up, access-controlled volume.

## Reporting

See [SECURITY.md](../../SECURITY.md) at the repository root.
