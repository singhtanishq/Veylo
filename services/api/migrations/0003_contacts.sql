-- 0003: Contacts, contact requests and blocking.

CREATE TABLE contacts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, contact_id),
    CONSTRAINT contacts_no_self CHECK (owner_id <> contact_id)
);

CREATE INDEX idx_contacts_owner   ON contacts (owner_id);
CREATE INDEX idx_contacts_contact ON contacts (contact_id);

CREATE TABLE contact_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    addressee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','accepted','declined','cancelled')),
    message      TEXT CHECK (message IS NULL OR char_length(message) <= 300),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at TIMESTAMPTZ,
    UNIQUE (requester_id, addressee_id),
    CONSTRAINT request_no_self CHECK (requester_id <> addressee_id)
);

CREATE INDEX idx_requests_addressee ON contact_requests (addressee_id) WHERE status = 'pending';
CREATE INDEX idx_requests_requester ON contact_requests (requester_id) WHERE status = 'pending';

CREATE TABLE blocked_users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (blocker_id, blocked_id),
    CONSTRAINT block_no_self CHECK (blocker_id <> blocked_id)
);

CREATE INDEX idx_blocked_blocker ON blocked_users (blocker_id);
CREATE INDEX idx_blocked_blocked ON blocked_users (blocked_id);
