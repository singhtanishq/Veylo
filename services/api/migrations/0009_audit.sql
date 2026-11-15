-- 0009: Audit log — security-relevant events with metadata only (no secrets,
-- no message contents, no tokens).

CREATE TABLE audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    action     TEXT NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    metadata   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_user_time ON audit_logs (user_id, created_at DESC);
CREATE INDEX idx_audit_action    ON audit_logs (action, created_at DESC);
