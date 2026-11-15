-- 0002: Sessions — server-side authenticated sessions with device metadata.
-- Session tokens are stored as SHA-256 hashes only; raw tokens live solely in
-- the HTTP-only cookie.

CREATE TABLE sessions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash     TEXT NOT NULL UNIQUE,
    device_label   TEXT NOT NULL DEFAULT 'Web browser',
    device_type    TEXT NOT NULL DEFAULT 'web',
    user_agent     TEXT,
    ip_address     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at     TIMESTAMPTZ NOT NULL,
    revoked_at     TIMESTAMPTZ,
    CONSTRAINT sessions_device_type CHECK (device_type IN ('web','mobile','desktop','unknown')),
    CONSTRAINT sessions_device_label CHECK (char_length(device_label) BETWEEN 1 AND 120)
);

CREATE INDEX idx_sessions_user_active  ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_sessions_expiry       ON sessions (expires_at) WHERE revoked_at IS NULL;
CREATE INDEX idx_sessions_token_lookup ON sessions (token_hash) WHERE revoked_at IS NULL;
