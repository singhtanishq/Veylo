-- 0008: Notifications and per-user preferences / privacy settings.

CREATE TABLE notifications (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type           TEXT NOT NULL
                   CHECK (type IN ('new_message','mention','group_invite','contact_request','contact_accepted','system_alert')),
    title          TEXT NOT NULL,
    body           TEXT,
    conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE,
    message_id     UUID REFERENCES messages(id) ON DELETE CASCADE,
    actor_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    read_at        TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_user_unread ON notifications (user_id, created_at DESC) WHERE read_at IS NULL;
CREATE INDEX idx_notifications_user_all    ON notifications (user_id, created_at DESC);

CREATE TABLE user_preferences (
    user_id                   UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    theme                     TEXT NOT NULL DEFAULT 'system' CHECK (theme IN ('light','dark','system')),
    notify_messages           BOOLEAN NOT NULL DEFAULT TRUE,
    notify_mentions           BOOLEAN NOT NULL DEFAULT TRUE,
    notify_contact_requests   BOOLEAN NOT NULL DEFAULT TRUE,
    notify_group_invites      BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_privacy_settings (
    user_id                 UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    last_seen_visibility    TEXT NOT NULL DEFAULT 'contacts'
                            CHECK (last_seen_visibility IN ('everyone','contacts','nobody')),
    online_visibility       TEXT NOT NULL DEFAULT 'contacts'
                            CHECK (online_visibility IN ('everyone','contacts','nobody')),
    profile_visibility      TEXT NOT NULL DEFAULT 'everyone'
                            CHECK (profile_visibility IN ('everyone','contacts')),
    read_receipts_enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    discoverable_by_email   BOOLEAN NOT NULL DEFAULT TRUE,
    discoverable_by_phone   BOOLEAN NOT NULL DEFAULT TRUE,
    who_can_send_requests   TEXT NOT NULL DEFAULT 'everyone'
                            CHECK (who_can_send_requests IN ('everyone','contacts_of_contacts','nobody')),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
