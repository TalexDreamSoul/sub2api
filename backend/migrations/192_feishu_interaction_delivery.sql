-- Feishu interaction receipts and reliable notification delivery.

INSERT INTO settings (key, value)
VALUES
    ('feishu_notify_verification_token', ''),
    ('feishu_notify_encrypt_key', '')
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS feishu_notification_outbox (
    id BIGSERIAL PRIMARY KEY,
    dedupe_key TEXT NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    recipient_open_id TEXT,
    app_id TEXT NOT NULL,
    category TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    provider_message_id TEXT,
    last_error TEXT,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    CONSTRAINT feishu_notification_outbox_dedupe_uq UNIQUE (dedupe_key),
    CONSTRAINT feishu_notification_outbox_status_check
        CHECK (status IN ('pending', 'processing', 'sent', 'dead')),
    CONSTRAINT feishu_notification_outbox_category_check
        CHECK (category IN ('test', 'admin_service', 'bot_reply', 'balance', 'subscription', 'quota', 'security', 'channel'))
);

CREATE INDEX IF NOT EXISTS idx_feishu_notification_outbox_claim
    ON feishu_notification_outbox (status, available_at, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_feishu_notification_outbox_user_created
    ON feishu_notification_outbox (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feishu_event_receipts (
    id BIGSERIAL PRIMARY KEY,
    app_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    tenant_key TEXT NOT NULL DEFAULT '',
    sender_open_id TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_sha256 TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    last_error TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT feishu_event_receipts_event_uq UNIQUE (app_id, event_id),
    CONSTRAINT feishu_event_receipts_status_check
        CHECK (status IN ('pending', 'processing', 'processed', 'ignored', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_feishu_event_receipts_pending
    ON feishu_event_receipts (status, available_at, id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS idx_feishu_event_receipts_received
    ON feishu_event_receipts (received_at DESC);
