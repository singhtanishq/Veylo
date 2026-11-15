-- 0004: Conversations — direct + group conversations, membership, sequences,
-- canonical direct-pair keys, per-conversation settings and group invitations.

CREATE TABLE conversations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type                TEXT NOT NULL CHECK (type IN ('direct','group')),
    title               TEXT,
    description         TEXT,
    avatar_attachment_id UUID,
    created_by          UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at     TIMESTAMPTZ,
    CONSTRAINT conv_title CHECK (title IS NULL OR (char_length(title) BETWEEN 1 AND 100)),
    CONSTRAINT conv_description CHECK (description IS NULL OR char_length(description) <= 500),
    CONSTRAINT conv_group_needs_title CHECK (type = 'direct' OR title IS NOT NULL)
);

CREATE INDEX idx_conversations_last_message ON conversations (last_message_at DESC NULLS LAST);
CREATE INDEX idx_conversations_title_trgm   ON conversations USING gin (title gin_trgm_ops);

CREATE TABLE conversation_members (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id    UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role               TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
    is_muted           BOOLEAN NOT NULL DEFAULT FALSE,
    is_pinned          BOOLEAN NOT NULL DEFAULT FALSE,
    is_archived        BOOLEAN NOT NULL DEFAULT FALSE,
    notification_level TEXT NOT NULL DEFAULT 'all'
                       CHECK (notification_level IN ('all','mentions_only','none')),
    joined_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    left_at            TIMESTAMPTZ,
    UNIQUE (conversation_id, user_id)
);

CREATE INDEX idx_members_user ON conversation_members (user_id) WHERE left_at IS NULL;
CREATE INDEX idx_members_conv ON conversation_members (conversation_id) WHERE left_at IS NULL;

-- Canonical pair key for one-to-one conversations: "uuidA:uuidB" with
-- uuidA < uuidB lexicographically. Guarantees a single direct conversation
-- per unordered pair.
CREATE TABLE direct_conversation_keys (
    conversation_id UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    pair_key        TEXT NOT NULL UNIQUE
);

-- Transactionally-safe per-conversation message sequence allocation.
CREATE TABLE conversation_sequences (
    conversation_id UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    last_seq        BIGINT NOT NULL DEFAULT 0
);

-- Per-conversation global policy (edit windows, who may invite).
CREATE TABLE conversation_settings (
    conversation_id     UUID PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    edit_window_seconds INT NOT NULL DEFAULT 900,
    members_can_invite  BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_invitations (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    inviter_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invitee_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending','accepted','declined','cancelled')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at   TIMESTAMPTZ,
    UNIQUE (conversation_id, invitee_id)
);

CREATE INDEX idx_invitations_invitee ON group_invitations (invitee_id) WHERE status = 'pending';
