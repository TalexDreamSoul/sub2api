-- Feishu group assistant administrators, chat bindings, and reliable chat delivery.

CREATE TABLE IF NOT EXISTS feishu_assistant_admins (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    configured_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS feishu_chat_bindings (
    id BIGSERIAL PRIMARY KEY,
    app_id TEXT NOT NULL,
    tenant_key TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL,
    chat_name TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'unconfigured',
    sub2api_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    incident_notifications_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    daily_digest_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    configured_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_at TIMESTAMPTZ,
    CONSTRAINT feishu_chat_bindings_external_uq UNIQUE (app_id, tenant_key, chat_id),
    CONSTRAINT feishu_chat_bindings_kind_check
        CHECK (kind IN ('unconfigured', 'user', 'operations', 'management', 'notifications')),
    CONSTRAINT feishu_chat_bindings_status_check
        CHECK (status IN ('pending', 'active', 'disabled')),
    CONSTRAINT feishu_chat_bindings_group_check
        CHECK (
            (kind IN ('user', 'operations') AND sub2api_group_id IS NOT NULL)
            OR kind IN ('unconfigured', 'management', 'notifications')
        )
);

CREATE INDEX IF NOT EXISTS idx_feishu_chat_bindings_active_kind
    ON feishu_chat_bindings (app_id, kind, id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_feishu_chat_bindings_group
    ON feishu_chat_bindings (sub2api_group_id, id)
    WHERE status = 'active' AND sub2api_group_id IS NOT NULL;

ALTER TABLE feishu_notification_outbox
    ADD COLUMN IF NOT EXISTS recipient_chat_id TEXT;

CREATE INDEX IF NOT EXISTS idx_feishu_notification_outbox_chat_created
    ON feishu_notification_outbox (recipient_chat_id, created_at DESC)
    WHERE recipient_chat_id IS NOT NULL;
