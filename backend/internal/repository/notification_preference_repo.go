package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type notificationPreferenceRepository struct{ db *sql.DB }

func NewNotificationPreferenceRepository(db *sql.DB) service.NotificationPreferenceRepository {
	return &notificationPreferenceRepository{db: db}
}

func (r *notificationPreferenceRepository) Get(ctx context.Context, userID int64, channel string, categories []string) (map[string]bool, error) {
	result := make(map[string]bool, len(categories))
	for _, category := range categories {
		result[category] = true
	}
	rows, err := r.db.QueryContext(ctx, `SELECT category,enabled FROM user_notification_preferences WHERE user_id=$1 AND channel=$2 AND category=ANY($3)`, userID, strings.TrimSpace(channel), pq.Array(categories))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var category string
		var enabled bool
		if err := rows.Scan(&category, &enabled); err != nil {
			return nil, err
		}
		result[category] = enabled
	}
	return result, rows.Err()
}

func (r *notificationPreferenceRepository) Set(ctx context.Context, userID int64, channel string, preferences map[string]bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	allowed := make(map[string]struct{}, len(service.FeishuNotificationCategories))
	for _, category := range service.FeishuNotificationCategories {
		allowed[category] = struct{}{}
	}
	for category, enabled := range preferences {
		if _, ok := allowed[category]; !ok {
			return fmt.Errorf("invalid notification category %q", category)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_notification_preferences(user_id,channel,category,enabled) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,channel,category) DO UPDATE SET enabled=EXCLUDED.enabled,updated_at=NOW()`, userID, strings.TrimSpace(channel), category, enabled); err != nil {
			return err
		}
	}
	return tx.Commit()
}
