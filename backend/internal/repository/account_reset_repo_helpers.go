package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountResetRepository) ClaimLocalPending(ctx context.Context, workerID string, limit int, staleAfter time.Duration) ([]service.AccountResetOperation, error) {
	if limit <= 0 {
		limit = 20
	}
	staleSeconds := int64(staleAfter / time.Second)
	if staleSeconds < 30 {
		staleSeconds = 60
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM account_reset_operations
			WHERE status IN ('upstream_succeeded','local_pending') OR (status='processing_local' AND updated_at < NOW()-($3*INTERVAL '1 second'))
			ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE account_reset_operations o SET status='processing_local',claimed_by=$1,updated_at=NOW()
		FROM candidates c WHERE o.id=c.id
		RETURNING o.id,o.operation_key,o.account_id,o.reset_type,o.status,o.restore_subscription_usage,COALESCE(o.upstream_redeem_request_id,''),o.created_by
	`, strings.TrimSpace(workerID), limit, staleSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.AccountResetOperation, 0, limit)
	for rows.Next() {
		var op service.AccountResetOperation
		var actor sql.NullInt64
		if err := rows.Scan(&op.ID, &op.OperationKey, &op.AccountID, &op.ResetType, &op.Status, &op.RestoreSubscriptionUsage, &op.UpstreamRedeemRequestID, &actor); err != nil {
			return nil, err
		}
		if actor.Valid {
			v := actor.Int64
			op.CreatedBy = &v
		}
		items = append(items, op)
	}
	return items, rows.Err()
}

func decodeAccountResetUpstreamResult(op *service.AccountResetOperation, raw sql.NullString) error {
	if op == nil || !raw.Valid || strings.TrimSpace(raw.String) == "" || raw.String == "null" {
		return nil
	}
	var result service.OpenAIQuotaResetResult
	if err := json.Unmarshal([]byte(raw.String), &result); err != nil {
		return fmt.Errorf("decode account reset upstream result: %w", err)
	}
	op.UpstreamResult = &result
	return nil
}

func loadAccountResetRefund(ctx context.Context, tx *sql.Tx, operationID int64) (*service.AccountResetRefundResult, error) {
	rows, err := tx.QueryContext(ctx, `SELECT subscription_id,user_id,group_id,refunded_daily_usd,refunded_weekly_usd,refunded_monthly_usd FROM account_reset_subscription_adjustments WHERE operation_id=$1 ORDER BY subscription_id`, operationID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := &service.AccountResetRefundResult{Adjustments: []service.AccountResetAdjustment{}}
	for rows.Next() {
		var item service.AccountResetAdjustment
		if err := rows.Scan(&item.SubscriptionID, &item.UserID, &item.GroupID, &item.DailyRefunded, &item.WeeklyRefunded, &item.MonthlyRefunded); err != nil {
			return nil, err
		}
		result.Adjustments = append(result.Adjustments, item)
		result.DailyRefundedUSD += item.DailyRefunded
		result.WeeklyRefundedUSD += item.WeeklyRefunded
		result.MonthlyRefundedUSD += item.MonthlyRefunded
	}
	return result, rows.Err()
}

func nullTimeArg(value sql.NullTime) any {
	if value.Valid {
		return value.Time
	}
	return nil
}
func accountResetWindowRefund(observed, currentUsage float64) float64 {
	return math.Min(math.Max(0, currentUsage), math.Max(0, observed))
}

func safeResetErrorCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "operation_failed"
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}
