package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type feishuChatBindingRepository struct {
	db *sql.DB
}

func NewFeishuChatBindingRepository(db *sql.DB) service.FeishuChatBindingRepository {
	return &feishuChatBindingRepository{db: db}
}

func (r *feishuChatBindingRepository) ListAdmins(ctx context.Context) ([]service.FeishuAssistantAdmin, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT administrator.user_id, user_account.email, user_account.username,
		       administrator.configured_by_user_id, administrator.created_at
		FROM feishu_assistant_admins administrator
		JOIN users user_account ON user_account.id = administrator.user_id
		ORDER BY administrator.created_at ASC, administrator.user_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuAssistantAdmin, 0)
	for rows.Next() {
		var item service.FeishuAssistantAdmin
		var configuredBy sql.NullInt64
		if err := rows.Scan(&item.UserID, &item.Email, &item.Username, &configuredBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if configuredBy.Valid {
			value := configuredBy.Int64
			item.ConfiguredByID = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *feishuChatBindingRepository) AddAdmin(ctx context.Context, userID, configuredByUserID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO feishu_assistant_admins (user_id, configured_by_user_id)
		VALUES ($1, NULLIF($2, 0))
		ON CONFLICT (user_id) DO UPDATE
		SET configured_by_user_id = EXCLUDED.configured_by_user_id
	`, userID, configuredByUserID)
	return err
}

func (r *feishuChatBindingRepository) RemoveAdmin(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM feishu_assistant_admins WHERE user_id = $1`, userID)
	return err
}

func (r *feishuChatBindingRepository) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM feishu_assistant_admins WHERE user_id = $1)`, userID).Scan(&exists)
	return exists, err
}

const feishuChatSelect = `
	SELECT binding.id, binding.app_id, binding.tenant_key, binding.chat_id, binding.chat_name,
	       binding.kind, binding.sub2api_group_id, COALESCE(linked_group.name, ''), binding.status,
	       binding.incident_notifications_enabled, binding.daily_digest_enabled,
	       binding.configured_by_user_id, binding.created_at, binding.updated_at, binding.disabled_at
	FROM feishu_chat_bindings binding
	LEFT JOIN groups linked_group ON linked_group.id = binding.sub2api_group_id
`

func (r *feishuChatBindingRepository) ListChats(ctx context.Context) ([]service.FeishuChatBinding, error) {
	rows, err := r.db.QueryContext(ctx, feishuChatSelect+` ORDER BY binding.updated_at DESC, binding.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuChatBinding, 0)
	for rows.Next() {
		item, err := scanFeishuChatBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *feishuChatBindingRepository) GetChatByID(ctx context.Context, id int64) (*service.FeishuChatBinding, error) {
	return r.getChat(ctx, feishuChatSelect+` WHERE binding.id = $1`, id)
}

func (r *feishuChatBindingRepository) GetChat(ctx context.Context, appID, tenantKey, chatID string) (*service.FeishuChatBinding, error) {
	return r.getChat(ctx, feishuChatSelect+` WHERE binding.app_id = $1 AND binding.tenant_key = $2 AND binding.chat_id = $3`,
		strings.TrimSpace(appID), strings.TrimSpace(tenantKey), strings.TrimSpace(chatID))
}

func (r *feishuChatBindingRepository) UpsertPendingChat(ctx context.Context, appID, tenantKey, chatID, chatName string) (*service.FeishuChatBinding, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO feishu_chat_bindings (app_id, tenant_key, chat_id, chat_name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (app_id, tenant_key, chat_id) DO UPDATE
		SET chat_name = CASE WHEN EXCLUDED.chat_name <> '' THEN EXCLUDED.chat_name ELSE feishu_chat_bindings.chat_name END,
		    updated_at = NOW()
		RETURNING id
	`, strings.TrimSpace(appID), strings.TrimSpace(tenantKey), strings.TrimSpace(chatID), strings.TrimSpace(chatName)).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetChatByID(ctx, id)
}

func (r *feishuChatBindingRepository) ConfigureChat(ctx context.Context, input service.ConfigureFeishuChatInput) (*service.FeishuChatBinding, error) {
	groupID := any(nil)
	if input.Sub2APIGroupID > 0 {
		groupID = input.Sub2APIGroupID
	}
	if input.ID > 0 {
		result, err := r.db.ExecContext(ctx, `
			UPDATE feishu_chat_bindings
			SET chat_name = $2, kind = $3, sub2api_group_id = $4, status = 'active',
			    incident_notifications_enabled = $5, daily_digest_enabled = $6,
			    configured_by_user_id = NULLIF($7, 0), disabled_at = NULL, updated_at = NOW()
			WHERE id = $1
		`, input.ID, strings.TrimSpace(input.ChatName), strings.TrimSpace(input.Kind), groupID,
			input.IncidentNotificationsEnabled, input.DailyDigestEnabled, input.ConfiguredByUserID)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, service.ErrFeishuChatNotConfigured
		}
		return r.GetChatByID(ctx, input.ID)
	}

	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO feishu_chat_bindings (
			app_id, tenant_key, chat_id, chat_name, kind, sub2api_group_id, status,
			incident_notifications_enabled, daily_digest_enabled, configured_by_user_id
		) VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8, NULLIF($9, 0))
		ON CONFLICT (app_id, tenant_key, chat_id) DO UPDATE
		SET chat_name = CASE WHEN EXCLUDED.chat_name <> '' THEN EXCLUDED.chat_name ELSE feishu_chat_bindings.chat_name END,
		    kind = EXCLUDED.kind, sub2api_group_id = EXCLUDED.sub2api_group_id, status = 'active',
		    incident_notifications_enabled = EXCLUDED.incident_notifications_enabled,
		    daily_digest_enabled = EXCLUDED.daily_digest_enabled,
		    configured_by_user_id = EXCLUDED.configured_by_user_id,
		    disabled_at = NULL, updated_at = NOW()
		RETURNING id
	`, strings.TrimSpace(input.AppID), strings.TrimSpace(input.TenantKey), strings.TrimSpace(input.ChatID),
		strings.TrimSpace(input.ChatName), strings.TrimSpace(input.Kind), groupID,
		input.IncidentNotificationsEnabled, input.DailyDigestEnabled, input.ConfiguredByUserID).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetChatByID(ctx, id)
}

func (r *feishuChatBindingRepository) DisableChat(ctx context.Context, appID, tenantKey, chatID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE feishu_chat_bindings
		SET status = 'disabled', incident_notifications_enabled = FALSE,
		    daily_digest_enabled = FALSE, disabled_at = NOW(), updated_at = NOW()
		WHERE app_id = $1 AND tenant_key = $2 AND chat_id = $3
	`, strings.TrimSpace(appID), strings.TrimSpace(tenantKey), strings.TrimSpace(chatID))
	return err
}

func (r *feishuChatBindingRepository) ListActiveChats(ctx context.Context, kinds []string) ([]service.FeishuChatBinding, error) {
	rows, err := r.db.QueryContext(ctx, feishuChatSelect+`
		WHERE binding.status = 'active' AND binding.kind = ANY($1)
		ORDER BY binding.id ASC
	`, pq.Array(kinds))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuChatBinding, 0)
	for rows.Next() {
		item, err := scanFeishuChatBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *feishuChatBindingRepository) getChat(ctx context.Context, query string, args ...any) (*service.FeishuChatBinding, error) {
	item, err := scanFeishuChatBinding(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrFeishuChatNotConfigured
	}
	return item, err
}

type feishuChatBindingScanner interface {
	Scan(dest ...any) error
}

func scanFeishuChatBinding(scanner feishuChatBindingScanner) (*service.FeishuChatBinding, error) {
	var item service.FeishuChatBinding
	var groupID, configuredBy sql.NullInt64
	var disabledAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.AppID, &item.TenantKey, &item.ChatID, &item.ChatName,
		&item.Kind, &groupID, &item.Sub2APIGroupName, &item.Status,
		&item.IncidentNotificationsEnabled, &item.DailyDigestEnabled,
		&configuredBy, &item.CreatedAt, &item.UpdatedAt, &disabledAt,
	); err != nil {
		return nil, err
	}
	if groupID.Valid {
		value := groupID.Int64
		item.Sub2APIGroupID = &value
	}
	if configuredBy.Valid {
		value := configuredBy.Int64
		item.ConfiguredByUserID = &value
	}
	if disabledAt.Valid {
		value := disabledAt.Time
		item.DisabledAt = &value
	}
	return &item, nil
}
