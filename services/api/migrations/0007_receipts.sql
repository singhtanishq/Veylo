-- 0007: Receipts — per-user-per-conversation delivery/read cursors plus
-- per-message receipt timestamps. Cursors are authoritative for unread
-- counts; receipts provide per-message timestamps for the UI.

CREATE TABLE message_read_cursors (
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    delivered_seq   BIGINT NOT NULL DEFAULT 0,
    read_seq        BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX idx_cursors_user ON message_read_cursors (user_id);

CREATE TABLE message_receipts (
    message_id   UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    delivered_at TIMESTAMPTZ,
    read_at      TIMESTAMPTZ,
    PRIMARY KEY (message_id, user_id)
);

CREATE INDEX idx_receipts_message ON message_receipts (message_id);
CREATE INDEX idx_receipts_user    ON message_receipts (user_id, read_at);
