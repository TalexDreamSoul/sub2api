package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type feishuNotificationOutboxRepository struct {
	db *sql.DB
}

func NewFeishuNotificationOutboxRepository(db *sql.DB) service.FeishuNotificationOutboxRepository {
	return &feishuNotificationOutboxRepository{db: db}
}

func (r *feishuNotificationOutboxRepository) Enqueue(ctx context.Context, input service.FeishuNotificationOutboxInput) (int64, bool, error) {
	if r == nil || r.db == nil {
		return 0, false, errors.New("nil feishu notification outbox database")
	}
	userID := any(nil)
	if input.UserID > 0 {
		userID = input.UserID
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO feishu_notification_outbox (
			dedupe_key, ordering_key, user_id, recipient_open_id, app_id, category, payload, created_by
		) VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8)
		ON CONFLICT (dedupe_key) DO NOTHING
		RETURNING id
	`, strings.TrimSpace(input.DedupeKey), strings.TrimSpace(input.OrderingKey), userID,
		strings.TrimSpace(input.RecipientOpenID), strings.TrimSpace(input.AppID), strings.TrimSpace(input.Category), input.Payload, input.CreatedBy).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT id FROM feishu_notification_outbox WHERE dedupe_key = $1
	`, strings.TrimSpace(input.DedupeKey)).Scan(&id); err != nil {
		return 0, false, err
	}
	return id, false, nil
}

func (r *feishuNotificationOutboxRepository) Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.FeishuNotificationOutboxItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil feishu notification outbox database")
	}
	if limit <= 0 {
		limit = 20
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 30
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT item.id
			FROM feishu_notification_outbox item
			WHERE item.available_at <= NOW()
			  AND (
				item.status = 'pending'
				OR (item.status = 'processing' AND item.claimed_at < NOW() - ($3 * INTERVAL '1 second'))
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM feishu_notification_outbox prior
				WHERE item.ordering_key IS NOT NULL
				  AND prior.ordering_key = item.ordering_key
				  AND prior.id < item.id
				  AND prior.status IN ('pending','processing')
			  )
			ORDER BY item.id ASC
			LIMIT $2
			FOR UPDATE OF item SKIP LOCKED
		)
		UPDATE feishu_notification_outbox AS o
		SET status = 'processing', claimed_at = NOW(), claimed_by = $1, updated_at = NOW()
		FROM candidates AS c
		WHERE o.id = c.id
		RETURNING o.id, o.user_id, COALESCE(o.recipient_open_id, ''), o.app_id, o.category, o.payload, o.attempts, o.created_at
	`, strings.TrimSpace(workerID), limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.FeishuNotificationOutboxItem, 0, limit)
	for rows.Next() {
		var item service.FeishuNotificationOutboxItem
		var userID sql.NullInt64
		if err := rows.Scan(&item.ID, &userID, &item.RecipientOpenID, &item.AppID, &item.Category, &item.Payload, &item.Attempts, &item.CreatedAt); err != nil {
			return nil, err
		}
		if userID.Valid {
			item.UserID = userID.Int64
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *feishuNotificationOutboxRepository) MarkSent(ctx context.Context, id int64, workerID, providerMessageID string) error {
	return r.updateClaimed(ctx, id, workerID, `
		UPDATE feishu_notification_outbox
		SET status = 'sent', provider_message_id = $3, sent_at = NOW(),
			payload = '{}'::jsonb, recipient_open_id = NULL,
			claimed_at = NULL, claimed_by = NULL, last_error = NULL, updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND status = 'processing'
	`, strings.TrimSpace(providerMessageID))
}

func (r *feishuNotificationOutboxRepository) Retry(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error {
	return r.updateClaimed(ctx, id, workerID, `
		UPDATE feishu_notification_outbox
		SET status = 'pending', attempts = attempts + 1, available_at = $3,
			last_error = $4, claimed_at = NULL, claimed_by = NULL, updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND status = 'processing'
	`, availableAt, truncateFeishuDeliveryError(lastError))
}

func (r *feishuNotificationOutboxRepository) MarkDead(ctx context.Context, id int64, workerID, lastError string) error {
	return r.updateClaimed(ctx, id, workerID, `
		UPDATE feishu_notification_outbox
		SET status = 'dead', attempts = attempts + 1, last_error = $3,
			payload = '{}'::jsonb, recipient_open_id = NULL,
			claimed_at = NULL, claimed_by = NULL, updated_at = NOW()
		WHERE id = $1 AND claimed_by = $2 AND status = 'processing'
	`, truncateFeishuDeliveryError(lastError))
}

func (r *feishuNotificationOutboxRepository) updateClaimed(ctx context.Context, id int64, workerID, query string, args ...any) error {
	params := []any{id, strings.TrimSpace(workerID)}
	params = append(params, args...)
	result, err := r.db.ExecContext(ctx, query, params...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("feishu outbox claim %d is no longer owned by %s", id, workerID)
	}
	return nil
}

func (r *feishuNotificationOutboxRepository) ListRecent(ctx context.Context, limit int) ([]service.FeishuNotificationDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, category, status, attempts, provider_message_id,
			(last_error IS NOT NULL), created_at, sent_at
		FROM feishu_notification_outbox
		ORDER BY created_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuNotificationDelivery, 0, limit)
	for rows.Next() {
		var item service.FeishuNotificationDelivery
		var userID sql.NullInt64
		var messageID sql.NullString
		var sentAt sql.NullTime
		var hasError bool
		if err := rows.Scan(&item.ID, &userID, &item.Category, &item.Status, &item.Attempts,
			&messageID, &hasError, &item.CreatedAt, &sentAt); err != nil {
			return nil, err
		}
		if userID.Valid {
			item.UserID = userID.Int64
		}
		if messageID.Valid {
			item.ProviderMessageID = messageID.String
		}
		if sentAt.Valid {
			value := sentAt.Time
			item.SentAt = &value
		}
		if hasError {
			item.ErrorCode = "delivery_failed"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *feishuNotificationOutboxRepository) CleanupTerminal(ctx context.Context, sentBefore, deadBefore time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM feishu_notification_outbox
		WHERE (status='sent' AND sent_at < $1)
		   OR (status='dead' AND updated_at < $2)`, sentBefore, deadBefore)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *feishuNotificationOutboxRepository) Stats(ctx context.Context) (service.FeishuNotificationOutboxStats, error) {
	var stats service.FeishuNotificationOutboxStats
	var oldest sql.NullTime
	var lastError sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'processing'),
			COUNT(*) FILTER (WHERE status = 'dead'),
			MIN(created_at) FILTER (WHERE status IN ('pending', 'processing')),
			(SELECT last_error FROM feishu_notification_outbox
			 WHERE last_error IS NOT NULL ORDER BY updated_at DESC, id DESC LIMIT 1)
		FROM feishu_notification_outbox
	`).Scan(&stats.Pending, &stats.Processing, &stats.Dead, &oldest, &lastError)
	if err != nil {
		return stats, err
	}
	if oldest.Valid {
		value := oldest.Time
		stats.OldestCreatedAt = &value
	}
	if lastError.Valid {
		stats.LastError = lastError.String
	}
	return stats, nil
}

func truncateFeishuDeliveryError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
