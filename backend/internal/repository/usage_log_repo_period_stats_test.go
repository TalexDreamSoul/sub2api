package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestGetAccountPeriodStatsBatch_ReturnsAllWindowsAndZeroRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 7, 23, 15, 30, 0, 0, time.UTC)
	lastUsed := now.Add(-time.Hour)
	rows := sqlmock.NewRows([]string{
		"account_id",
		"today_requests", "today_tokens", "today_cost", "today_standard_cost", "today_user_cost",
		"week_requests", "week_tokens", "week_cost", "week_standard_cost", "week_user_cost",
		"month_requests", "month_tokens", "month_cost", "month_standard_cost", "month_user_cost",
		"last_used_at",
	}).AddRow(
		int64(11),
		int64(2), int64(100), 1.2, 1.0, 1.5,
		int64(8), int64(500), 4.2, 4.0, 5.0,
		int64(30), int64(2000), 15.2, 14.0, 18.0,
		lastUsed,
	)
	mock.ExpectQuery(`(?s)SELECT\s+account_id.*FILTER \(WHERE created_at >= \$2\).*FROM usage_logs.*GROUP BY account_id`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(rows)

	repo := &usageLogRepository{sql: db}
	stats, err := repo.GetAccountPeriodStatsBatch(context.Background(), []int64{11, 12}, now)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	require.EqualValues(t, 2, stats[11].Today.Requests)
	require.EqualValues(t, 8, stats[11].Last7Days.Requests)
	require.EqualValues(t, 30, stats[11].Last30Days.Requests)
	require.NotNil(t, stats[11].LastUsedAt)
	require.WithinDuration(t, lastUsed, *stats[11].LastUsedAt, time.Second)

	require.NotNil(t, stats[12])
	require.Zero(t, stats[12].Today.Requests)
	require.Nil(t, stats[12].LastUsedAt)
}

func TestGetAccountPeriodStatsBatch_EmptyInputSkipsQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &usageLogRepository{sql: db}
	stats, err := repo.GetAccountPeriodStatsBatch(context.Background(), nil, time.Now())
	require.NoError(t, err)
	require.Empty(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}
