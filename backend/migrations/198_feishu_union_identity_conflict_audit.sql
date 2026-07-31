-- Make union identity ownership race-free within one Feishu app/tenant.
-- Preserve a compact audit trail for duplicates created by the former
-- check-then-write flow. Existing ambiguous bindings are never deleted by an
-- automatic migration; operators can resolve them after reviewing the audit.
CREATE TABLE IF NOT EXISTS feishu_identity_binding_conflicts (
    removed_binding_id BIGINT PRIMARY KEY,
    kept_binding_id BIGINT NOT NULL,
    removed_user_id BIGINT NOT NULL,
    kept_user_id BIGINT NOT NULL,
    app_id TEXT NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

WITH ranked AS (
    SELECT
        id,
        user_id,
        app_id,
        FIRST_VALUE(id) OVER ownership AS kept_binding_id,
        FIRST_VALUE(user_id) OVER ownership AS kept_user_id,
        ROW_NUMBER() OVER ownership AS ownership_rank
    FROM user_feishu_identity_bindings
    WHERE union_id <> ''
    WINDOW ownership AS (
        PARTITION BY app_id, tenant_key, union_id, purpose
        ORDER BY id
    )
)
INSERT INTO feishu_identity_binding_conflicts (
    removed_binding_id, kept_binding_id, removed_user_id, kept_user_id, app_id
)
SELECT id, kept_binding_id, user_id, kept_user_id, app_id
FROM ranked
WHERE ownership_rank > 1
ON CONFLICT (removed_binding_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS user_feishu_identity_bindings_app_tenant_union_purpose_idx
    ON user_feishu_identity_bindings (app_id, tenant_key, union_id, purpose)
    WHERE union_id <> '';
