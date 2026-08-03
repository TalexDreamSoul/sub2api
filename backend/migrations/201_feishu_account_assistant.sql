-- Feishu account assistant configuration, API key requests, and daily digests.

INSERT INTO settings (key, value)
VALUES (
    'feishu_assistant_config',
    '{"enabled":false,"api_key_id":0,"model":"","daily_digest_enabled":false,"daily_digest_time":"00:05","api_key_request_mode":"manual","default_group_id":0,"max_active_keys":5}'
)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS feishu_api_key_requests (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    requested_name TEXT NOT NULL,
    source_event_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    reviewed_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    review_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at TIMESTAMPTZ,
    CONSTRAINT feishu_api_key_requests_source_event_uq UNIQUE (source_event_id),
    CONSTRAINT feishu_api_key_requests_status_check
        CHECK (status IN ('pending', 'processing', 'issued', 'rejected', 'cancelled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS feishu_api_key_requests_one_open_per_user_idx
    ON feishu_api_key_requests (user_id)
    WHERE status IN ('pending', 'processing');

CREATE INDEX IF NOT EXISTS feishu_api_key_requests_status_created_idx
    ON feishu_api_key_requests (status, created_at DESC, id DESC);

ALTER TABLE user_notification_preferences
    DROP CONSTRAINT IF EXISTS user_notification_preferences_category_check;
ALTER TABLE user_notification_preferences
    ADD CONSTRAINT user_notification_preferences_category_check
    CHECK (category IN ('balance','subscription','quota','security','channel','daily_digest'));

ALTER TABLE feishu_notification_outbox
    DROP CONSTRAINT IF EXISTS feishu_notification_outbox_category_check;
ALTER TABLE feishu_notification_outbox
    ADD CONSTRAINT feishu_notification_outbox_category_check
    CHECK (category IN ('test','admin_service','bot_reply','balance','subscription','quota','security','channel','daily_digest'));
