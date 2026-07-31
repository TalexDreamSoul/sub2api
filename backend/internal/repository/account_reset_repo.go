package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountResetRepository struct{ db *sql.DB }

func NewAccountResetRepository(db *sql.DB) service.AccountResetRepository {
	return &accountResetRepository{db: db}
}

func (r *accountResetRepository) Begin(ctx context.Context, key string, accountID int64, resetType string, restore bool, redeemID string, createdBy *int64) (*service.AccountResetOperation, bool, error) {
	var op service.AccountResetOperation
	var actor sql.NullInt64
	var upstreamResult sql.NullString
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO account_reset_operations (operation_key, account_id, reset_type, restore_subscription_usage, upstream_redeem_request_id, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6)
		ON CONFLICT (operation_key) DO NOTHING
		RETURNING id, operation_key, account_id, reset_type, status, restore_subscription_usage,
			COALESCE(upstream_redeem_request_id,''), upstream_result::text, created_by
	`, strings.TrimSpace(key), accountID, resetType, restore, strings.TrimSpace(redeemID), createdBy).
		Scan(&op.ID, &op.OperationKey, &op.AccountID, &op.ResetType, &op.Status, &op.RestoreSubscriptionUsage, &op.UpstreamRedeemRequestID, &upstreamResult, &actor)
	if err == nil {
		if err := decodeAccountResetUpstreamResult(&op, upstreamResult); err != nil {
			return nil, false, err
		}
		if actor.Valid {
			op.CreatedBy = &actor.Int64
		}
		return &op, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	err = r.db.QueryRowContext(ctx, `
		SELECT id, operation_key, account_id, reset_type, status, restore_subscription_usage,
			COALESCE(upstream_redeem_request_id,''), upstream_result::text, created_by
		FROM account_reset_operations WHERE operation_key=$1
	`, strings.TrimSpace(key)).Scan(&op.ID, &op.OperationKey, &op.AccountID, &op.ResetType, &op.Status, &op.RestoreSubscriptionUsage, &op.UpstreamRedeemRequestID, &upstreamResult, &actor)
	if err != nil {
		return nil, false, err
	}
	if err := decodeAccountResetUpstreamResult(&op, upstreamResult); err != nil {
		return nil, false, err
	}
	if actor.Valid {
		op.CreatedBy = &actor.Int64
	}
	if op.AccountID != accountID || op.ResetType != resetType || op.RestoreSubscriptionUsage != restore {
		return nil, false, service.ErrAccountResetIdempotencyConflict
	}
	return &op, false, nil
}

func (r *accountResetRepository) ResetLocalQuotaAndMarkSucceeded(ctx context.Context, operationID, accountID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var storedAccountID int64
	var resetType, status string
	if err := tx.QueryRowContext(ctx, `SELECT account_id,reset_type,status FROM account_reset_operations WHERE id=$1 FOR UPDATE`, operationID).Scan(&storedAccountID, &resetType, &status); err != nil {
		return err
	}
	if storedAccountID != accountID || resetType != service.AccountResetTypeLocalQuota {
		return fmt.Errorf("account reset operation %d does not match local account reset", operationID)
	}
	if status != "pending" {
		switch status {
		case "upstream_succeeded", "local_pending", "processing_local", "completed":
			return tx.Commit()
		default:
			return fmt.Errorf("account reset operation %d is not pending", operationID)
		}
	}

	result, err := tx.ExecContext(ctx, `UPDATE accounts SET extra = (
		COALESCE(extra, '{}'::jsonb)
		|| '{"quota_used": 0, "quota_daily_used": 0, "quota_weekly_used": 0}'::jsonb
	) - 'quota_daily_start' - 'quota_weekly_start' - 'quota_daily_reset_at' - 'quota_weekly_reset_at', updated_at=NOW()
	WHERE id=$1 AND deleted_at IS NULL`, accountID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("account %d is not available for quota reset", accountID)
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `UPDATE account_reset_operations SET status='upstream_succeeded',upstream_succeeded_at=COALESCE(upstream_succeeded_at,NOW()),last_error_code=NULL,updated_at=NOW() WHERE id=$1 AND status='pending'`, operationID)
	if err != nil {
		return err
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("account reset operation %d changed concurrently", operationID)
	}
	return tx.Commit()
}

func (r *accountResetRepository) MarkUpstreamSucceeded(ctx context.Context, id int64, upstream *service.OpenAIQuotaResetResult) error {
	var payload any
	if upstream != nil {
		raw, err := json.Marshal(upstream)
		if err != nil {
			return fmt.Errorf("encode account reset upstream result: %w", err)
		}
		payload = string(raw)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE account_reset_operations
		SET status='upstream_succeeded', upstream_succeeded_at=COALESCE(upstream_succeeded_at,NOW()),
			upstream_result=COALESCE(upstream_result,$2::jsonb), last_error_code=NULL, updated_at=NOW()
		WHERE id=$1 AND status IN ('pending','upstream_succeeded')`, id, payload)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("account reset operation %d is not pending", id)
	}
	return nil
}
func (r *accountResetRepository) MarkLocalPending(ctx context.Context, id int64, code string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE account_reset_operations SET status='local_pending', claimed_by=NULL, last_error_code=$2, updated_at=NOW() WHERE id=$1 AND status IN ('upstream_succeeded','local_pending','processing_local')`, id, safeResetErrorCode(code))
	return err
}
func (r *accountResetRepository) MarkFailed(ctx context.Context, id int64, code string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE account_reset_operations SET status='failed', claimed_by=NULL, last_error_code=$2, updated_at=NOW(), completed_at=NOW() WHERE id=$1 AND status <> 'completed'`, id, safeResetErrorCode(code))
	return err
}
func (r *accountResetRepository) CompleteWithoutRefund(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE account_reset_operations SET status='completed', claimed_by=NULL, last_error_code=NULL, completed_at=NOW(), updated_at=NOW() WHERE id=$1 AND status IN ('upstream_succeeded','local_pending','processing_local','completed')`, id)
	return err
}

func (r *accountResetRepository) ApplySubscriptionRefund(ctx context.Context, operationID int64) (*service.AccountResetRefundResult, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var accountID int64
	var status string
	var restore bool
	var observedThrough sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT account_id,status,restore_subscription_usage,upstream_succeeded_at FROM account_reset_operations WHERE id=$1 FOR UPDATE`, operationID).Scan(&accountID, &status, &restore, &observedThrough); err != nil {
		return nil, err
	}
	if status == "completed" {
		result, err := loadAccountResetRefund(ctx, tx, operationID)
		if err != nil {
			return nil, err
		}
		return result, tx.Commit()
	}
	if status != "upstream_succeeded" && status != "local_pending" && status != "processing_local" {
		return nil, fmt.Errorf("account reset operation %d is not ready for local refund", operationID)
	}
	if !restore {
		if _, err := tx.ExecContext(ctx, `UPDATE account_reset_operations SET status='completed',claimed_by=NULL,last_error_code=NULL,completed_at=NOW(),updated_at=NOW() WHERE id=$1`, operationID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &service.AccountResetRefundResult{Adjustments: []service.AccountResetAdjustment{}}, nil
	}

	if !observedThrough.Valid {
		return nil, fmt.Errorf("account reset operation %d has no upstream success cutoff", operationID)
	}

	// Compatibility for retained usage created before the authoritative ledger
	// was deployed. Scope the backfill to this account and the longest quota
	// window so reset processing never performs an unbounded startup copy.
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_account_usage_ledger (
		request_id,api_key_id,account_id,subscription_id,actual_cost_usd,occurred_at
	)
	SELECT request_id,api_key_id,account_id,subscription_id,actual_cost,created_at
	FROM usage_logs
	WHERE account_id=$1 AND request_id IS NOT NULL AND subscription_id IS NOT NULL
	  AND billing_type=1 AND actual_cost>=0
	  AND created_at BETWEEN $2::timestamptz-INTERVAL '35 days' AND $2
	ON CONFLICT (request_id,api_key_id) DO NOTHING`, accountID, observedThrough.Time); err != nil {
		return nil, fmt.Errorf("backfill account reset ledger: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT us.id,us.user_id,us.group_id,us.daily_window_start,us.weekly_window_start,us.monthly_window_start,
			us.daily_usage_usd,us.weekly_usage_usd,us.monthly_usage_usd
		FROM user_subscriptions us
		WHERE EXISTS (
			SELECT 1 FROM subscription_account_usage_ledger l
			WHERE l.subscription_id=us.id AND l.account_id=$1
			  AND l.reset_operation_id IS NULL AND l.occurred_at <= $2
		)
		ORDER BY us.id FOR UPDATE
	`, accountID, observedThrough.Time)
	if err != nil {
		return nil, err
	}
	type subRow struct {
		id, userID, groupID             int64
		dayStart, weekStart, monthStart sql.NullTime
		dayUsed, weekUsed, monthUsed    float64
	}
	subs := make([]subRow, 0)
	for rows.Next() {
		var s subRow
		if err := rows.Scan(&s.id, &s.userID, &s.groupID, &s.dayStart, &s.weekStart, &s.monthStart, &s.dayUsed, &s.weekUsed, &s.monthUsed); err != nil {
			_ = rows.Close()
			return nil, err
		}
		subs = append(subs, s)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, sub := range subs {
		var observedDay, observedWeek, observedMonth float64
		err := tx.QueryRowContext(ctx, `WITH settled AS (
			UPDATE subscription_account_usage_ledger
			SET reset_operation_id=$1, settled_at=NOW()
			WHERE account_id=$2 AND subscription_id=$3
			  AND reset_operation_id IS NULL AND occurred_at <= $7
			RETURNING actual_cost_usd, occurred_at
		)
		SELECT
			COALESCE(SUM(actual_cost_usd) FILTER (WHERE $4::timestamptz IS NOT NULL AND occurred_at >= $4),0),
			COALESCE(SUM(actual_cost_usd) FILTER (WHERE $5::timestamptz IS NOT NULL AND occurred_at >= $5),0),
			COALESCE(SUM(actual_cost_usd) FILTER (WHERE $6::timestamptz IS NOT NULL AND occurred_at >= $6),0)
		FROM settled`, operationID, accountID, sub.id, nullTimeArg(sub.dayStart), nullTimeArg(sub.weekStart), nullTimeArg(sub.monthStart), observedThrough.Time).Scan(&observedDay, &observedWeek, &observedMonth)
		if err != nil {
			return nil, err
		}
		refDay := accountResetWindowRefund(observedDay, sub.dayUsed)
		refWeek := accountResetWindowRefund(observedWeek, sub.weekUsed)
		refMonth := accountResetWindowRefund(observedMonth, sub.monthUsed)
		if _, err = tx.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=GREATEST(0,daily_usage_usd-$2),weekly_usage_usd=GREATEST(0,weekly_usage_usd-$3),monthly_usage_usd=GREATEST(0,monthly_usage_usd-$4),updated_at=NOW() WHERE id=$1`, sub.id, refDay, refWeek, refMonth); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_reset_subscription_adjustments (operation_id,account_id,subscription_id,user_id,group_id,daily_window_start,weekly_window_start,monthly_window_start,observed_daily_contribution_usd,observed_weekly_contribution_usd,observed_monthly_contribution_usd,refunded_daily_usd,refunded_weekly_usd,refunded_monthly_usd,observed_through_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, operationID, accountID, sub.id, sub.userID, sub.groupID, nullTimeArg(sub.dayStart), nullTimeArg(sub.weekStart), nullTimeArg(sub.monthStart), observedDay, observedWeek, observedMonth, refDay, refWeek, refMonth, observedThrough.Time); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_reset_operations SET status='completed',claimed_by=NULL,last_error_code=NULL,completed_at=NOW(),updated_at=NOW() WHERE id=$1`, operationID); err != nil {
		return nil, err
	}
	result, err := loadAccountResetRefund(ctx, tx, operationID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
