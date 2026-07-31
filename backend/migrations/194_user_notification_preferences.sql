-- Per-user notification category preferences.
CREATE TABLE IF NOT EXISTS user_notification_preferences (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    category TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_notification_preferences_channel_check CHECK (channel IN ('feishu')),
    CONSTRAINT user_notification_preferences_category_check CHECK (category IN ('balance','subscription','quota','security','channel')),
    CONSTRAINT user_notification_preferences_user_channel_category_uq UNIQUE (user_id, channel, category)
);
CREATE INDEX IF NOT EXISTS idx_user_notification_preferences_user_channel
    ON user_notification_preferences (user_id, channel);
