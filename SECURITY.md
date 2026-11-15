# Security Policy

## Supported versions

Veylo is a local-first development project. Security fixes are applied to the
latest commit on `main`.

## Reporting a vulnerability

If you discover a security vulnerability in Veylo, please open a GitHub issue
marked `security` describing the problem **without disclosing exploit details
in public**, or contact the repository owner privately.

## Security model overview

Veylo is designed for **local development environments**:

- Passwords are hashed with **Argon2id** (never stored in plaintext).
- Sessions are **server-side**, referenced by an opaque random token stored as
  an HTTP-only cookie. Only a SHA-256 hash of the token is persisted.
- All conversation, message and attachment access is **membership-authorized**
  on the server (IDOR protection).
- File uploads are validated server-side (size limits, MIME signature checks,
  sanitized server-generated storage keys) and are only downloadable by
  conversation members.
- Rate limiting protects authentication, search and messaging endpoints.
- User-generated content is rendered as plain text (React default escaping);
  no HTML injection paths exist by design.

## What Veylo is NOT (yet)

- Messages are **not end-to-end encrypted**. Transport between the browser and
  the local API is plain HTTP on localhost. E2E encryption is a documented
  roadmap item, not a current claim.
- HTTPS is not enabled locally. If you expose Veylo on a network, put it
  behind TLS and review cookie settings (`secure` flag) before doing so.

## Deployment notes

The defaults target `localhost`. Before any non-local deployment:

1. Generate real secrets (`APP_ENCRYPTION_KEY`, `SESSION_SECRET`).
2. Enable TLS termination and set secure cookie flags.
3. Review rate limits, CORS origins and storage permissions.
4. Configure real (authenticated) SMTP instead of Mailpit.
