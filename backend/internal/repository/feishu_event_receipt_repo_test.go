package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFeishuEventReceiptReceiveDeduplicatesByAppAndEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	payload := json.RawMessage(`{"header":{"event_id":"evt-1"}}`)
	mock.ExpectQuery(`(?s)INSERT INTO feishu_event_receipts.*ON CONFLICT \(app_id, event_id\) DO NOTHING.*RETURNING id`).
		WithArgs("cli-app", "evt-1", "im.message.receive_v1", "tenant", "ou-1", payload, "sha").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(8)))

	repo := NewFeishuEventReceiptRepository(db)
	id, inserted, err := repo.Receive(context.Background(), service.FeishuEventReceiptInput{
		AppID: "cli-app", EventID: "evt-1", EventType: "im.message.receive_v1",
		TenantKey: "tenant", SenderOpenID: "ou-1", Payload: payload, PayloadSHA256: "sha",
	})
	require.NoError(t, err)
	require.True(t, inserted)
	require.EqualValues(t, 8, id)
	require.NoError(t, mock.ExpectationsWereMet())
}
