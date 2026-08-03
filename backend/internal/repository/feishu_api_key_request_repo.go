package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type feishuAPIKeyRequestRepository struct {
	db *sql.DB
}

func NewFeishuAPIKeyRequestRepository(db *sql.DB) service.FeishuAPIKeyRequestRepository {
	return &feishuAPIKeyRequestRepository{db: db}
}

func (r *feishuAPIKeyRequestRepository) Create(ctx context.Context, input service.CreateFeishuAPIKeyRequestInput) (*service.FeishuAPIKeyRequest, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, errors.New("nil Feishu API key request database")
	}
	request, err := scanFeishuAPIKeyRequest(r.db.QueryRowContext(ctx, `
INSERT INTO feishu_api_key_requests (
    user_id, requested_group_id, requested_name, source_event_id
) VALUES ($1, $2, $3, $4)
RETURNING id, user_id, requested_group_id, requested_name, source_event_id,
          status, api_key_id, reviewed_by, review_note, created_at, updated_at, decided_at`,
		input.UserID, input.RequestedGroupID, strings.TrimSpace(input.RequestedName), strings.TrimSpace(input.SourceEventID)))
	if err == nil {
		return request, true, nil
	}
	if !isFeishuUniqueViolation(err) {
		return nil, false, err
	}
	request, lookupErr := r.getBySourceEvent(ctx, input.SourceEventID)
	if lookupErr == nil {
		return request, false, nil
	}
	if !errors.Is(lookupErr, sql.ErrNoRows) {
		return nil, false, lookupErr
	}
	return nil, false, service.ErrFeishuAPIKeyRequestBusy
}

func (r *feishuAPIKeyRequestRepository) Get(ctx context.Context, id int64) (*service.FeishuAPIKeyRequest, error) {
	request, err := scanFeishuAPIKeyRequest(r.db.QueryRowContext(ctx, `
SELECT id, user_id, requested_group_id, requested_name, source_event_id,
       status, api_key_id, reviewed_by, review_note, created_at, updated_at, decided_at
FROM feishu_api_key_requests WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrFeishuAPIKeyRequestNotFound
	}
	return request, err
}

func (r *feishuAPIKeyRequestRepository) getBySourceEvent(ctx context.Context, eventID string) (*service.FeishuAPIKeyRequest, error) {
	return scanFeishuAPIKeyRequest(r.db.QueryRowContext(ctx, `
SELECT id, user_id, requested_group_id, requested_name, source_event_id,
       status, api_key_id, reviewed_by, review_note, created_at, updated_at, decided_at
FROM feishu_api_key_requests WHERE source_event_id=$1`, strings.TrimSpace(eventID)))
}

func (r *feishuAPIKeyRequestRepository) List(ctx context.Context, status string, limit int) ([]service.FeishuAPIKeyRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, user_id, requested_group_id, requested_name, source_event_id,
       status, api_key_id, reviewed_by, review_note, created_at, updated_at, decided_at
FROM feishu_api_key_requests
WHERE ($1='' OR status=$1)
ORDER BY created_at DESC, id DESC
LIMIT $2`, strings.TrimSpace(status), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.FeishuAPIKeyRequest, 0, limit)
	for rows.Next() {
		item, err := scanFeishuAPIKeyRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *feishuAPIKeyRequestRepository) Claim(ctx context.Context, id int64) (*service.FeishuAPIKeyRequest, error) {
	request, err := scanFeishuAPIKeyRequest(r.db.QueryRowContext(ctx, `
UPDATE feishu_api_key_requests
SET status='processing', updated_at=NOW()
WHERE id=$1 AND status='pending'
RETURNING id, user_id, requested_group_id, requested_name, source_event_id,
          status, api_key_id, reviewed_by, review_note, created_at, updated_at, decided_at`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrFeishuAPIKeyRequestBusy
	}
	return request, err
}

func (r *feishuAPIKeyRequestRepository) ResetPending(ctx context.Context, id int64, note string) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE feishu_api_key_requests
SET status='pending', review_note=$2, updated_at=NOW()
WHERE id=$1 AND status='processing'`, id, strings.TrimSpace(note))
	return requireFeishuAPIKeyRequestUpdate(result, err)
}

func (r *feishuAPIKeyRequestRepository) MarkIssued(ctx context.Context, id, apiKeyID int64, reviewedBy *int64) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE feishu_api_key_requests
SET status='issued', api_key_id=$2, reviewed_by=$3, review_note='',
    decided_at=NOW(), updated_at=NOW()
WHERE id=$1 AND status='processing'`, id, apiKeyID, reviewedBy)
	return requireFeishuAPIKeyRequestUpdate(result, err)
}

func (r *feishuAPIKeyRequestRepository) Reject(ctx context.Context, id int64, reviewedBy int64, note string) (*service.FeishuAPIKeyRequest, error) {
	request, err := scanFeishuAPIKeyRequest(r.db.QueryRowContext(ctx, `
UPDATE feishu_api_key_requests
SET status='rejected', reviewed_by=$2, review_note=$3,
    decided_at=NOW(), updated_at=NOW()
WHERE id=$1 AND status='pending'
RETURNING id, user_id, requested_group_id, requested_name, source_event_id,
          status, api_key_id, reviewed_by, review_note, created_at, updated_at, decided_at`,
		id, reviewedBy, strings.TrimSpace(note)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrFeishuAPIKeyRequestBusy
	}
	return request, err
}

type feishuAPIKeyRequestScanner interface {
	Scan(dest ...any) error
}

func scanFeishuAPIKeyRequest(scanner feishuAPIKeyRequestScanner) (*service.FeishuAPIKeyRequest, error) {
	var item service.FeishuAPIKeyRequest
	var apiKeyID, reviewedBy sql.NullInt64
	var decidedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.UserID, &item.RequestedGroupID, &item.RequestedName, &item.SourceEventID,
		&item.Status, &apiKeyID, &reviewedBy, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt, &decidedAt,
	); err != nil {
		return nil, err
	}
	if apiKeyID.Valid {
		value := apiKeyID.Int64
		item.APIKeyID = &value
	}
	if reviewedBy.Valid {
		value := reviewedBy.Int64
		item.ReviewedBy = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time
		item.DecidedAt = &value
	}
	return &item, nil
}

func requireFeishuAPIKeyRequestUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrFeishuAPIKeyRequestBusy
	}
	return nil
}
