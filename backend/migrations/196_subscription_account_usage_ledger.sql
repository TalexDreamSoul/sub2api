-- Authoritative account-attributed subscription usage ledger.
-- Rows are written in the same transaction as subscription usage increments,
-- so reset refunds do not depend on best-effort usage_logs retention.

CREATE TABLE IF NOT EXISTS subscription_account_usage_ledger (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    actual_cost_usd DECIMAL(20, 10) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reset_operation_id BIGINT REFERENCES account_reset_operations(id) ON DELETE RESTRICT,
    settled_at TIMESTAMPTZ,
    CONSTRAINT subscription_account_usage_ledger_cost_check CHECK (actual_cost_usd >= 0),
    CONSTRAINT subscription_account_usage_ledger_request_uq UNIQUE (request_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS idx_subscription_account_usage_unsettled
    ON subscription_account_usage_ledger (account_id, subscription_id, occurred_at, id)
    WHERE reset_operation_id IS NULL;

-- Retained pre-ledger usage is backfilled lazily for one account and one reset
-- cutoff inside ApplySubscriptionRefund. Keeping that work out of startup
-- migrations avoids an unbounded usage_logs scan and large WAL burst.
