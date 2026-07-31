package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AccountResetTypeLocalQuota   = "local_quota"
	AccountResetTypeOpenAICredit = "openai_credit"
)

var ErrAccountResetIdempotencyConflict = infraerrors.Conflict(
	"ACCOUNT_RESET_IDEMPOTENCY_CONFLICT",
	"idempotency key was already used with different reset parameters",
)

type AccountResetOperation struct {
	ID                       int64
	OperationKey             string
	AccountID                int64
	ResetType                string
	Status                   string
	RestoreSubscriptionUsage bool
	UpstreamRedeemRequestID  string
	UpstreamResult           *OpenAIQuotaResetResult
	CreatedBy                *int64
}

type AccountResetAdjustment struct {
	SubscriptionID  int64   `json:"subscription_id"`
	UserID          int64   `json:"user_id"`
	GroupID         int64   `json:"group_id"`
	DailyRefunded   float64 `json:"daily_refunded_usd"`
	WeeklyRefunded  float64 `json:"weekly_refunded_usd"`
	MonthlyRefunded float64 `json:"monthly_refunded_usd"`
}

type AccountResetRefundResult struct {
	Adjustments        []AccountResetAdjustment `json:"adjustments"`
	DailyRefundedUSD   float64                  `json:"daily_refunded_usd"`
	WeeklyRefundedUSD  float64                  `json:"weekly_refunded_usd"`
	MonthlyRefundedUSD float64                  `json:"monthly_refunded_usd"`
}

type AccountResetResult struct {
	OperationID              int64                    `json:"operation_id"`
	Status                   string                   `json:"status"`
	RestoreSubscriptionUsage bool                     `json:"restore_subscription_usage"`
	Refund                   AccountResetRefundResult `json:"refund"`
}

type AccountResetRepository interface {
	Begin(ctx context.Context, operationKey string, accountID int64, resetType string, restore bool, redeemRequestID string, createdBy *int64) (*AccountResetOperation, bool, error)
	MarkUpstreamSucceeded(ctx context.Context, operationID int64, result *OpenAIQuotaResetResult) error
	ResetLocalQuotaAndMarkSucceeded(ctx context.Context, operationID, accountID int64) error
	MarkLocalPending(ctx context.Context, operationID int64, errorCode string) error
	MarkFailed(ctx context.Context, operationID int64, errorCode string) error
	CompleteWithoutRefund(ctx context.Context, operationID int64) error
	ApplySubscriptionRefund(ctx context.Context, operationID int64) (*AccountResetRefundResult, error)
	ClaimLocalPending(ctx context.Context, workerID string, limit int, staleAfter time.Duration) ([]AccountResetOperation, error)
}
