package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFeishuNotificationOutboxEnqueueIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	payload := json.RawMessage(`{"config":{"wide_screen_mode":true}}`)
	mock.ExpectQuery(`(?s)INSERT INTO feishu_notification_outbox.*ON CONFLICT \(dedupe_key\) DO NOTHING.*RETURNING id`).
		WithArgs("admin:1:message:abc", "", int64(7), "", "cli_app", "admin_service", payload, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	repo := NewFeishuNotificationOutboxRepository(db)
	id, inserted, err := repo.Enqueue(context.Background(), service.FeishuNotificationOutboxInput{
		DedupeKey: "admin:1:message:abc",
		UserID:    7,
		AppID:     "cli_app",
		Category:  service.FeishuNotificationCategoryAdminService,
		Payload:   payload,
	})
	require.NoError(t, err)
	require.True(t, inserted)
	require.EqualValues(t, 42, id)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFeishuNotificationOutboxMarkDeadClearsSensitivePayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`(?s)UPDATE feishu_notification_outbox.*status = 'dead'.*payload = '\{\}'::jsonb, recipient_open_id = NULL.*WHERE id = \$1 AND claimed_by = \$2`).
		WithArgs(int64(5), "worker-1", "provider_error").
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewFeishuNotificationOutboxRepository(db)
	require.NoError(t, repo.MarkDead(context.Background(), 5, "worker-1", "provider_error"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFeishuNotificationOutboxClaimUsesLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	createdAt := time.Now().Add(-time.Minute)
	mock.ExpectQuery(`(?s)FOR UPDATE OF item SKIP LOCKED.*UPDATE feishu_notification_outbox AS o.*RETURNING`).
		WithArgs("worker-1", 10, int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "recipient_open_id", "app_id", "category", "payload", "attempts", "created_at"}).
			AddRow(int64(5), int64(7), "", "cli_app", "admin_service", []byte(`{"header":{}}`), 1, createdAt))

	repo := NewFeishuNotificationOutboxRepository(db)
	items, err := repo.Claim(context.Background(), "worker-1", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, 5, items[0].ID)
	require.JSONEq(t, `{"header":{}}`, string(items[0].Payload))
	require.NoError(t, mock.ExpectationsWereMet())
}
