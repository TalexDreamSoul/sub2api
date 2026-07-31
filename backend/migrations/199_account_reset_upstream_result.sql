-- Persist the sanitized OpenAI reset response so Idempotency-Key replays
-- return the same provider result instead of a fabricated zero-value success.
ALTER TABLE account_reset_operations
    ADD COLUMN IF NOT EXISTS upstream_result JSONB;
