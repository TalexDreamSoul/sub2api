-- Ensure operations that reached upstream success are recoverable after a
-- process crash before local subscription adjustments begin.

DROP INDEX IF EXISTS idx_account_reset_operations_retry;
CREATE INDEX idx_account_reset_operations_retry
    ON account_reset_operations (status, updated_at, id)
    WHERE status IN ('upstream_succeeded', 'local_pending', 'processing_local');
