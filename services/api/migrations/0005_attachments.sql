-- 0005: Attachments — metadata only; binaries live in local file storage
-- (storage/uploads).

CREATE TABLE attachments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    storage_key  TEXT NOT NULL UNIQUE,
    thumb_key    TEXT,
    file_name    TEXT NOT NULL,
    mime_type    TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('image','video','audio','voice','document')),
    size_bytes   BIGINT NOT NULL CHECK (size_bytes > 0),
    sha256       TEXT NOT NULL,
    width        INT,
    height       INT,
    duration_ms  INT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT attachment_name CHECK (char_length(file_name) BETWEEN 1 AND 200),
    CONSTRAINT attachment_mime CHECK (char_length(mime_type) BETWEEN 1 AND 100),
    CONSTRAINT attachment_dims CHECK ((width IS NULL AND height IS NULL) OR (width > 0 AND height > 0))
);

CREATE INDEX idx_attachments_owner ON attachments (owner_id);
CREATE INDEX idx_attachments_created ON attachments (created_at DESC);
