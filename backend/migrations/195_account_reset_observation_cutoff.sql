-- Preserve reset attribution across usage-log retention cleanup for databases
-- that applied migration 193 before observed_through_at was introduced.

ALTER TABLE account_reset_subscription_adjustments
    ADD COLUMN IF NOT EXISTS observed_through_at TIMESTAMPTZ;

UPDATE account_reset_subscription_adjustments
SET observed_through_at = created_at
WHERE observed_through_at IS NULL;

ALTER TABLE account_reset_subscription_adjustments
    ALTER COLUMN observed_through_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_account_reset_adjustments_observed_through
    ON account_reset_subscription_adjustments (account_id, subscription_id, observed_through_at);
