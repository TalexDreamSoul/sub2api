-- Durable account reset workflow and subscription contribution refund ledger.

CREATE TABLE IF NOT EXISTS account_reset_operations (
    id BIGSERIAL PRIMARY KEY,
    operation_key TEXT NOT NULL UNIQUE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    reset_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    restore_subscription_usage BOOLEAN NOT NULL DEFAULT FALSE,
    upstream_redeem_request_id TEXT,
    claimed_by TEXT,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    upstream_succeeded_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT account_reset_operations_type_check CHECK (reset_type IN ('local_quota', 'openai_credit')),
    CONSTRAINT account_reset_operations_status_check CHECK (status IN ('pending', 'upstream_succeeded', 'local_pending', 'processing_local', 'completed', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_account_reset_operations_account_created
    ON account_reset_operations (account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_reset_operations_retry
    ON account_reset_operations (status, updated_at, id)
    WHERE status IN ('upstream_succeeded', 'local_pending', 'processing_local');

CREATE TABLE IF NOT EXISTS account_reset_subscription_adjustments (
    id BIGSERIAL PRIMARY KEY,
    operation_id BIGINT NOT NULL REFERENCES account_reset_operations(id) ON DELETE RESTRICT,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    daily_window_start TIMESTAMPTZ,
    weekly_window_start TIMESTAMPTZ,
    monthly_window_start TIMESTAMPTZ,
    observed_daily_contribution_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    observed_weekly_contribution_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    observed_monthly_contribution_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    refunded_daily_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    refunded_weekly_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    refunded_monthly_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    observed_through_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_reset_subscription_adjustments_operation_sub_uq UNIQUE (operation_id, subscription_id)
);

CREATE INDEX IF NOT EXISTS idx_account_reset_adjustments_observed_through
    ON account_reset_subscription_adjustments (account_id, subscription_id, observed_through_at);
