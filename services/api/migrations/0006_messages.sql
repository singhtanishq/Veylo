-- 0006: Messages — persistent, server-ordered (per-conversation sequence),
-- idempotent sends via client_message_id, reactions, edits, attachments.

CREATE TABLE messages (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id   UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id         UUID REFERENCES users(id) ON DELETE SET NULL, -- NULL = system message
    type              TEXT NOT NULL DEFAULT 'text'
                      CHECK (type IN ('text','image','video','audio','voice','document','system')),
    text              TEXT,
    reply_to_id       UUID REFERENCES messages(id) ON DELETE SET NULL,
    seq               BIGINT NOT NULL,
    client_message_id UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at         TIMESTAMPTZ,
    deleted_at        TIMESTAMPTZ,
    deleted_by        UUID REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (conversation_id, seq),
    -- Text size guard (server-side, not just frontend validation).
    CONSTRAINT message_text_limit CHECK (text IS NULL OR octet_length(text) <= 20000),
    -- System messages never have a client id.
    CONSTRAINT message_system_shape CHECK (
        (type = 'system' AND sender_id IS NULL) OR (type <> 'system' AND sender_id IS NOT NULL)
    )
);

-- Idempotency: a retried send with the same client_message_id resolves to the
-- original row instead of duplicating it.
CREATE UNIQUE INDEX idx_messages_client_id
    ON messages (conversation_id, sender_id, client_message_id)
    WHERE client_message_id IS NOT NULL;

CREATE INDEX idx_messages_conv_seq  ON messages (conversation_id, seq DESC);
CREATE INDEX idx_messages_sender    ON messages (sender_id);

-- Full-text search over message text (simple config keeps search deterministic
-- across languages; weights stay default).
CREATE INDEX idx_messages_fts
    ON messages USING gin (to_tsvector('simple', text))
    WHERE deleted_at IS NULL AND type = 'text';

CREATE TABLE message_edits (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id    UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    previous_text TEXT NOT NULL,
    new_text      TEXT NOT NULL,
    edited_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_edits_message ON message_edits (message_id);

CREATE TABLE message_reactions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (message_id, user_id, emoji),
    CONSTRAINT reaction_emoji_limit CHECK (char_length(emoji) BETWEEN 1 AND 16)
);

CREATE INDEX idx_reactions_message ON message_reactions (message_id);

CREATE TABLE message_attachments (
    message_id    UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    position      INT NOT NULL DEFAULT 0,
    PRIMARY KEY (message_id, attachment_id)
);

CREATE INDEX idx_message_attachments_att ON message_attachments (attachment_id);
