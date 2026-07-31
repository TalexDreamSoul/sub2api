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

type feishuEventReceiptRepository struct {
	db *sql.DB
}

func NewFeishuEventReceiptRepository(db *sql.DB) service.FeishuEventReceiptRepository {
	return &feishuEventReceiptRepository{db: db}
}

func (r *feishuEventReceiptRepository) Receive(ctx context.Context, input service.FeishuEventReceiptInput) (int64, bool, error) {
	if r == nil || r.db == nil {
		return 0, false, errors.New("nil feishu event receipt database")
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO feishu_event_receipts (
			app_id, event_id, event_type, tenant_key, sender_open_id, payload, payload_sha256
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (app_id, event_id) DO NOTHING
		RETURNING id
	`, strings.TrimSpace(input.AppID), strings.TrimSpace(input.EventID), strings.TrimSpace(input.EventType),
		strings.TrimSpace(input.TenantKey), strings.TrimSpace(input.SenderOpenID), input.Payload, strings.TrimSpace(input.PayloadSHA256)).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT id FROM feishu_event_receipts WHERE app_id = $1 AND event_id = $2
	`, strings.TrimSpace(input.AppID), strings.TrimSpace(input.EventID)).Scan(&id); err != nil {
		return 0, false, err
	}
	return id, false, nil
}

func (r *feishuEventReceiptRepository) Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]service.FeishuEventReceipt, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("nil feishu event receipt database")
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
			SELECT id FROM feishu_event_receipts
			WHERE available_at <= NOW()
			  AND (status = 'pending' OR (status = 'processing' AND claimed_at < NOW() - ($3 * INTERVAL '1 second')))
			ORDER BY id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE feishu_event_receipts AS e
		SET status = 'processing', claimed_at = NOW(), claimed_by = $1
		FROM candidates AS c
		WHERE e.id = c.id
		RETURNING e.id, e.app_id, e.event_id, e.event_type, e.tenant_key,
			e.sender_open_id, e.payload, e.attempts
	`, strings.TrimSpace(workerID), limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuEventReceipt, 0, limit)
	for rows.Next() {
		var item service.FeishuEventReceipt
		if err := rows.Scan(&item.ID, &item.AppID, &item.EventID, &item.EventType, &item.TenantKey,
			&item.SenderOpenID, &item.Payload, &item.Attempts); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *feishuEventReceiptRepository) Complete(ctx context.Context, id int64, workerID, status string) error {
	status = strings.TrimSpace(status)
	if status != "processed" && status != "ignored" && status != "failed" {
		return fmt.Errorf("invalid feishu event completion status %q", status)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE feishu_event_receipts
		SET status = $3, processed_at = NOW(), payload = '{}'::jsonb,
			claimed_at = NULL, claimed_by = NULL
		WHERE id = $1 AND claimed_by = $2 AND status = 'processing'
	`, id, strings.TrimSpace(workerID), status)
	return requireSingleClaimUpdate(result, err, id, workerID, "feishu event")
}

func (r *feishuEventReceiptRepository) Retry(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE feishu_event_receipts
		SET status = 'pending', attempts = attempts + 1, available_at = $3,
			last_error = $4, claimed_at = NULL, claimed_by = NULL
		WHERE id = $1 AND claimed_by = $2 AND status = 'processing'
	`, id, strings.TrimSpace(workerID), availableAt, truncateFeishuDeliveryError(lastError))
	return requireSingleClaimUpdate(result, err, id, workerID, "feishu event")
}

func requireSingleClaimUpdate(result sql.Result, err error, id int64, workerID, kind string) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%s claim %d is no longer owned by %s", kind, id, workerID)
	}
	return nil
}
