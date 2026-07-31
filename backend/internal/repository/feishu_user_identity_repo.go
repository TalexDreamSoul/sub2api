package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type feishuUserIdentityRepository struct {
	db *sql.DB
}

func NewFeishuUserIdentityRepository(sqlDB *sql.DB) service.FeishuUserIdentityRepository {
	return &feishuUserIdentityRepository{db: sqlDB}
}

func (r *feishuUserIdentityRepository) UpsertFeishuUserIdentityBinding(ctx context.Context, input service.UpsertFeishuUserIdentityBindingInput) (*service.FeishuUserIdentityBinding, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrFeishuNotificationDisabled
	}
	input.AppID = strings.TrimSpace(input.AppID)
	input.TenantKey = strings.TrimSpace(input.TenantKey)
	input.OpenID = strings.TrimSpace(input.OpenID)
	input.UnionID = strings.TrimSpace(input.UnionID)
	input.Purpose = strings.TrimSpace(input.Purpose)
	if input.Purpose == "" {
		input.Purpose = service.FeishuIdentityPurposeNotify
	}
	metadata, err := json.Marshal(normalizeFeishuBindingMetadata(input.Metadata))
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if input.UnionID != "" {
		lockKey := strings.Join([]string{input.AppID, input.TenantKey, input.UnionID, input.Purpose}, "\x00")
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return nil, err
		}
		var totalOwners, matchingOwners int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*), COUNT(*) FILTER (WHERE user_id = $5)
FROM user_feishu_identity_bindings
WHERE app_id = $1 AND tenant_key = $2 AND union_id = $3 AND purpose = $4`, input.AppID, input.TenantKey, input.UnionID, input.Purpose, input.UserID).Scan(&totalOwners, &matchingOwners); err != nil {
			return nil, err
		}
		if totalOwners != matchingOwners {
			return nil, service.ErrFeishuNotificationConflict
		}
	}
	var binding service.FeishuUserIdentityBinding
	var rawMetadata []byte
	err = tx.QueryRowContext(ctx, `
INSERT INTO user_feishu_identity_bindings (
	user_id, app_id, tenant_key, open_id, union_id, purpose,
	notification_enabled, metadata, bound_at, last_seen_at, created_at, updated_at
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8::jsonb, NOW(), NOW(), NOW(), NOW()
)
ON CONFLICT (user_id, app_id, purpose)
DO UPDATE SET
	tenant_key = EXCLUDED.tenant_key,
	open_id = EXCLUDED.open_id,
	union_id = EXCLUDED.union_id,
	notification_enabled = EXCLUDED.notification_enabled,
	metadata = EXCLUDED.metadata,
	last_seen_at = NOW(),
	updated_at = NOW()
RETURNING user_id, app_id, tenant_key, open_id, union_id, purpose, notification_enabled, metadata, bound_at, last_seen_at`,
		input.UserID,
		input.AppID,
		input.TenantKey,
		input.OpenID,
		input.UnionID,
		input.Purpose,
		input.NotificationEnabled,
		string(metadata),
	).Scan(
		&binding.UserID,
		&binding.AppID,
		&binding.TenantKey,
		&binding.OpenID,
		&binding.UnionID,
		&binding.Purpose,
		&binding.NotificationEnabled,
		&rawMetadata,
		&binding.BoundAt,
		&binding.LastSeenAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isFeishuUniqueViolation(err) {
			return nil, service.ErrFeishuNotificationConflict
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	binding.Metadata = parseFeishuBindingMetadata(rawMetadata)
	return &binding, nil
}

func (r *feishuUserIdentityRepository) GetFeishuNotificationBinding(ctx context.Context, userID int64, appID string) (*service.FeishuUserIdentityBinding, error) {
	return r.getBinding(ctx, `
SELECT user_id, app_id, tenant_key, open_id, union_id, purpose, notification_enabled, metadata, bound_at, last_seen_at
FROM user_feishu_identity_bindings
WHERE user_id = $1 AND app_id = $2 AND purpose = $3
LIMIT 1`, userID, strings.TrimSpace(appID), service.FeishuIdentityPurposeNotify)
}

func (r *feishuUserIdentityRepository) GetFeishuBindingByUnionID(ctx context.Context, appID, tenantKey, unionID, purpose string) (*service.FeishuUserIdentityBinding, error) {
	return r.getBinding(ctx, `
SELECT user_id, app_id, tenant_key, open_id, union_id, purpose, notification_enabled, metadata, bound_at, last_seen_at
FROM user_feishu_identity_bindings
WHERE app_id = $1 AND tenant_key = $2 AND union_id = $3 AND purpose = $4
ORDER BY id
LIMIT 2`, strings.TrimSpace(appID), strings.TrimSpace(tenantKey), strings.TrimSpace(unionID), strings.TrimSpace(purpose))
}

func (r *feishuUserIdentityRepository) GetFeishuBindingByOpenID(ctx context.Context, appID, tenantKey, openID, purpose string) (*service.FeishuUserIdentityBinding, error) {
	return r.getBinding(ctx, `
SELECT user_id, app_id, tenant_key, open_id, union_id, purpose, notification_enabled, metadata, bound_at, last_seen_at
FROM user_feishu_identity_bindings
WHERE app_id = $1 AND tenant_key = $2 AND open_id = $3 AND purpose = $4
LIMIT 1`, strings.TrimSpace(appID), strings.TrimSpace(tenantKey), strings.TrimSpace(openID), strings.TrimSpace(purpose))
}

func (r *feishuUserIdentityRepository) ListFeishuBoundUsers(ctx context.Context, appID, search string, limit int) ([]service.FeishuBoundUser, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT u.id,u.email,COALESCE(u.username,'')
FROM user_feishu_identity_bindings b
JOIN users u ON u.id=b.user_id AND u.deleted_at IS NULL
WHERE b.app_id=$1 AND b.purpose='notify' AND b.notification_enabled=TRUE
  AND ($2='' OR u.email ILIKE '%' || $2 || '%' OR COALESCE(u.username,'') ILIKE '%' || $2 || '%' OR u.id::text=$2)
ORDER BY b.updated_at DESC,u.id
LIMIT $3`, strings.TrimSpace(appID), strings.TrimSpace(search), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuBoundUser, 0, limit)
	for rows.Next() {
		var item service.FeishuBoundUser
		if err := rows.Scan(&item.ID, &item.Email, &item.Username); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *feishuUserIdentityRepository) ListFeishuBindingsByUser(ctx context.Context, userID int64) ([]service.FeishuUserIdentityBinding, error) {
	if r == nil || r.db == nil {
		return []service.FeishuUserIdentityBinding{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT user_id, app_id, tenant_key, open_id, union_id, purpose, notification_enabled, metadata, bound_at, last_seen_at
FROM user_feishu_identity_bindings
WHERE user_id = $1
ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	bindings := make([]service.FeishuUserIdentityBinding, 0)
	for rows.Next() {
		binding, err := scanFeishuBinding(rows)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, *binding)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return bindings, nil
}

func (r *feishuUserIdentityRepository) SetFeishuNotificationEnabled(ctx context.Context, userID int64, appID string, enabled bool) (*service.FeishuUserIdentityBinding, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrFeishuNotificationNotBound
	}
	var binding service.FeishuUserIdentityBinding
	var rawMetadata []byte
	err := r.db.QueryRowContext(ctx, `
UPDATE user_feishu_identity_bindings
SET notification_enabled = $3, updated_at = NOW()
WHERE user_id = $1 AND app_id = $2 AND purpose = 'notify'
RETURNING user_id, app_id, tenant_key, open_id, union_id, purpose, notification_enabled, metadata, bound_at, last_seen_at`,
		userID, strings.TrimSpace(appID), enabled,
	).Scan(
		&binding.UserID,
		&binding.AppID,
		&binding.TenantKey,
		&binding.OpenID,
		&binding.UnionID,
		&binding.Purpose,
		&binding.NotificationEnabled,
		&rawMetadata,
		&binding.BoundAt,
		&binding.LastSeenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrFeishuNotificationNotBound
	}
	if err != nil {
		return nil, err
	}
	binding.Metadata = parseFeishuBindingMetadata(rawMetadata)
	return &binding, nil
}

func (r *feishuUserIdentityRepository) DeleteFeishuNotificationBinding(ctx context.Context, userID int64, appID string) error {
	if r == nil || r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
DELETE FROM user_feishu_identity_bindings
WHERE user_id = $1 AND app_id = $2 AND purpose = 'notify'`, userID, strings.TrimSpace(appID))
	return err
}

func (r *feishuUserIdentityRepository) getBinding(ctx context.Context, query string, args ...any) (*service.FeishuUserIdentityBinding, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrFeishuNotificationNotBound
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrFeishuNotificationNotBound
	}
	binding, err := scanFeishuBinding(rows)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, service.ErrFeishuNotificationConflict
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return binding, nil
}

type feishuBindingScanner interface {
	Scan(dest ...any) error
}

func scanFeishuBinding(scanner feishuBindingScanner) (*service.FeishuUserIdentityBinding, error) {
	var binding service.FeishuUserIdentityBinding
	var rawMetadata []byte
	if err := scanner.Scan(
		&binding.UserID,
		&binding.AppID,
		&binding.TenantKey,
		&binding.OpenID,
		&binding.UnionID,
		&binding.Purpose,
		&binding.NotificationEnabled,
		&rawMetadata,
		&binding.BoundAt,
		&binding.LastSeenAt,
	); err != nil {
		return nil, err
	}
	binding.Metadata = parseFeishuBindingMetadata(rawMetadata)
	return &binding, nil
}

func (r *feishuUserIdentityRepository) ListFeishuChannelRecipientUserIDs(ctx context.Context, appID string, afterUserID int64, limit int) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrFeishuNotificationDisabled
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := r.db.QueryContext(ctx, `SELECT binding.user_id
		FROM user_feishu_identity_bindings binding
		LEFT JOIN user_notification_preferences preference
		  ON preference.user_id=binding.user_id AND preference.channel='feishu' AND preference.category='channel'
		WHERE binding.app_id=$1 AND binding.purpose=$2 AND binding.notification_enabled=TRUE
		  AND binding.user_id>$3 AND COALESCE(preference.enabled,TRUE)=TRUE
		ORDER BY binding.user_id LIMIT $4`, strings.TrimSpace(appID), service.FeishuIdentityPurposeNotify, afterUserID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func normalizeFeishuBindingMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func parseFeishuBindingMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func isFeishuUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr != nil && pqErr.Code == "23505"
}
