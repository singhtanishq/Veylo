-- 0001: Core identity — users, profiles, phone numbers, verification tokens.

-- pg_trgm backs fuzzy username/email/display-name search indexes.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE users (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username           TEXT NOT NULL UNIQUE,
    username_display   TEXT NOT NULL,
    email              TEXT NOT NULL UNIQUE,
    password_hash      TEXT NOT NULL,
    account_status     TEXT NOT NULL DEFAULT 'pending_verification'
                       CHECK (account_status IN ('pending_verification','active','deactivated','disabled')),
    email_verified_at  TIMESTAMPTZ,
    last_seen_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- username: lowercase letters/digits/underscore, 3-24 chars (normalized at the application boundary)
    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9_]{3,24}$'),
    -- email stored normalized (lowercase); basic shape check
    CONSTRAINT users_email_format CHECK (email = lower(email) AND email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')
);

CREATE INDEX idx_users_username_trgm ON users USING gin (username gin_trgm_ops);
CREATE INDEX idx_users_email_trgm    ON users USING gin (email gin_trgm_ops);
CREATE INDEX idx_users_status        ON users (account_status);

CREATE TABLE user_phone_numbers (
    user_id       UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    phone_e164    TEXT,
    verified      BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at   TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT phone_format CHECK (phone_e164 IS NULL OR phone_e164 ~ '^\+[1-9][0-9]{6,14}$')
);

-- Only verified numbers participate in discovery; enforce global uniqueness
-- among *verified* numbers via a partial unique index.
CREATE UNIQUE INDEX idx_phone_verified_unique
    ON user_phone_numbers (phone_e164)
    WHERE verified = TRUE AND phone_e164 IS NOT NULL;

CREATE INDEX idx_phone_e164 ON user_phone_numbers (phone_e164) WHERE phone_e164 IS NOT NULL;

CREATE TABLE user_profiles (
    user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name         TEXT NOT NULL,
    about                TEXT,
    avatar_attachment_id UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT profile_display_name CHECK (char_length(display_name) BETWEEN 1 AND 80),
    CONSTRAINT profile_about CHECK (about IS NULL OR char_length(about) <= 280)
);

CREATE INDEX idx_profiles_display_name_trgm ON user_profiles USING gin (display_name gin_trgm_ops);

CREATE TABLE email_verification_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_evt_user ON email_verification_tokens (user_id);

CREATE TABLE password_reset_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_prt_user ON password_reset_tokens (user_id);
