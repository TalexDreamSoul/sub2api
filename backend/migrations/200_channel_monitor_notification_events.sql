-- Durable channel-monitor incident state and notification fanout queue.
CREATE TABLE IF NOT EXISTS channel_monitor_health_states (
    monitor_id BIGINT PRIMARY KEY REFERENCES channel_monitors(id) ON DELETE CASCADE,
    failure_streak INTEGER NOT NULL DEFAULT 0,
    incident_open BOOLEAN NOT NULL DEFAULT FALSE,
    incident_version BIGINT NOT NULL DEFAULT 0,
    last_observed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT channel_monitor_health_failure_streak_check CHECK (failure_streak >= 0),
    CONSTRAINT channel_monitor_health_incident_version_check CHECK (incident_version >= 0)
);

CREATE TABLE IF NOT EXISTS channel_monitor_notification_events (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT NOT NULL REFERENCES channel_monitors(id) ON DELETE CASCADE,
    incident_version BIGINT NOT NULL,
    event_kind TEXT NOT NULL,
    monitor_name TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    observed_status TEXT NOT NULL,
    latency_ms INTEGER,
    checked_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    last_error TEXT,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT channel_monitor_notification_event_kind_check CHECK (event_kind IN ('incident', 'recovery')),
    CONSTRAINT channel_monitor_notification_status_check CHECK (status IN ('pending', 'processing', 'sent', 'dead')),
    CONSTRAINT channel_monitor_notification_attempts_check CHECK (attempts >= 0),
    CONSTRAINT channel_monitor_notification_version_uq UNIQUE (monitor_id, incident_version, event_kind)
);

CREATE INDEX IF NOT EXISTS idx_channel_monitor_notification_claim
    ON channel_monitor_notification_events (status, available_at, id)
    WHERE status IN ('pending', 'processing');

ALTER TABLE feishu_notification_outbox
    ADD COLUMN IF NOT EXISTS ordering_key TEXT;

CREATE INDEX IF NOT EXISTS idx_feishu_notification_outbox_ordering
    ON feishu_notification_outbox (ordering_key, id)
    WHERE ordering_key IS NOT NULL AND status IN ('pending', 'processing');
