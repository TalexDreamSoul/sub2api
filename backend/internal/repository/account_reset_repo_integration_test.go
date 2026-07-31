//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type accountResetRefundFixture struct {
	userID         int64
	groupID        int64
	apiKeyID       int64
	accountID      int64
	subscriptionID int64
	accountIDs     []int64
	dayStart       time.Time
	weekStart      time.Time
	monthStart     time.Time
}

func newAccountResetRefundFixture(t *testing.T, daily, weekly, monthly float64) *accountResetRefundFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	suffix := uuid.NewString()
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("account-reset-%s@example.com", suffix),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "account-reset-group-" + suffix,
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-account-reset-" + suffix,
		Name:    "account-reset-key",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "account-reset-account-" + suffix,
		Type: service.AccountTypeAPIKey,
	})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:          user.ID,
		GroupID:         group.ID,
		DailyUsageUSD:   daily,
		WeeklyUsageUSD:  weekly,
		MonthlyUsageUSD: monthly,
	})

	now := time.Now().UTC()
	fixture := &accountResetRefundFixture{
		userID: user.ID, groupID: group.ID, apiKeyID: apiKey.ID,
		accountID: account.ID, subscriptionID: subscription.ID,
		accountIDs: []int64{account.ID},
		dayStart:   now.Add(-6 * time.Hour), weekStart: now.Add(-48 * time.Hour), monthStart: now.Add(-20 * 24 * time.Hour),
	}
	t.Cleanup(func() {
		ctx := context.Background()
		tx, err := integrationDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		accountIDs := pq.Array(fixture.accountIDs)
		for _, stmt := range []struct {
			query string
			arg   any
		}{
			{`DELETE FROM account_reset_subscription_adjustments WHERE account_id = ANY($1)`, accountIDs},
			{`DELETE FROM subscription_account_usage_ledger WHERE account_id = ANY($1)`, accountIDs},
			{`DELETE FROM account_reset_operations WHERE account_id = ANY($1)`, accountIDs},
			{`DELETE FROM usage_logs WHERE user_id = $1`, fixture.userID},
			{`DELETE FROM api_keys WHERE id = $1`, fixture.apiKeyID},
			{`DELETE FROM user_subscriptions WHERE id = $1`, fixture.subscriptionID},
			{`DELETE FROM accounts WHERE id = ANY($1)`, accountIDs},
			{`DELETE FROM groups WHERE id = $1`, fixture.groupID},
			{`DELETE FROM users WHERE id = $1`, fixture.userID},
		} {
			_, err = tx.ExecContext(ctx, stmt.query, stmt.arg)
			require.NoError(t, err)
		}
		require.NoError(t, tx.Commit())
	})
	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions
		SET daily_window_start=$2, weekly_window_start=$3, monthly_window_start=$4
		WHERE id=$1`, fixture.subscriptionID, fixture.dayStart, fixture.weekStart, fixture.monthStart)
	require.NoError(t, err)
	return fixture
}

func (f *accountResetRefundFixture) trackAccount(accountID int64) {
	f.accountIDs = append(f.accountIDs, accountID)
}

func (f *accountResetRefundFixture) addUsage(t *testing.T, accountID int64, billingType int8, cost float64, createdAt time.Time) string {
	t.Helper()
	requestID := uuid.NewString()
	_, err := testEntClient(t).UsageLog.Create().
		SetUserID(f.userID).
		SetAPIKeyID(f.apiKeyID).
		SetAccountID(accountID).
		SetGroupID(f.groupID).
		SetSubscriptionID(f.subscriptionID).
		SetRequestID(requestID).
		SetModel("claude-test").
		SetBillingType(billingType).
		SetTotalCost(cost).
		SetActualCost(cost).
		SetCreatedAt(createdAt).
		Save(context.Background())
	require.NoError(t, err)
	if billingType == service.BillingTypeSubscription {
		_, err = integrationDB.ExecContext(context.Background(), `INSERT INTO subscription_account_usage_ledger
			(request_id,api_key_id,account_id,subscription_id,actual_cost_usd,occurred_at)
			VALUES ($1,$2,$3,$4,$5,$6)`, requestID, f.apiKeyID, accountID, f.subscriptionID, cost, createdAt)
		require.NoError(t, err)
	}
	return requestID
}

func beginRefundOperation(t *testing.T, repo service.AccountResetRepository, accountID int64) int64 {
	t.Helper()
	op, created, err := repo.Begin(context.Background(), uuid.NewString(), accountID, service.AccountResetTypeLocalQuota, true, "", nil)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, repo.MarkUpstreamSucceeded(context.Background(), op.ID, nil))
	return op.ID
}

func readSubscriptionUsage(t *testing.T, subscriptionID int64) (float64, float64, float64) {
	t.Helper()
	var daily, weekly, monthly float64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=$1`, subscriptionID).Scan(&daily, &weekly, &monthly))
	return daily, weekly, monthly
}

func TestAccountResetRefundUsesAuthoritativeLedgerAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 10, 20, 30)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 4, time.Now().UTC().Add(-time.Minute))
	fixture.addUsage(t, fixture.accountID, service.BillingTypeBalance, 2, time.Now().UTC().Add(-time.Minute))
	other := mustCreateAccount(t, testEntClient(t), &service.Account{Name: "account-reset-other-" + uuid.NewString()})
	fixture.trackAccount(other.ID)
	fixture.addUsage(t, other.ID, service.BillingTypeSubscription, 3, time.Now().UTC().Add(-time.Minute))

	repo := NewAccountResetRepository(integrationDB)
	opID := beginRefundOperation(t, repo, fixture.accountID)
	result, err := repo.ApplySubscriptionRefund(ctx, opID)
	require.NoError(t, err)
	require.InDelta(t, 4, result.DailyRefundedUSD, 0.000001)
	require.InDelta(t, 4, result.WeeklyRefundedUSD, 0.000001)
	require.InDelta(t, 4, result.MonthlyRefundedUSD, 0.000001)

	daily, weekly, monthly := readSubscriptionUsage(t, fixture.subscriptionID)
	require.InDelta(t, 6, daily, 0.000001)
	require.InDelta(t, 16, weekly, 0.000001)
	require.InDelta(t, 26, monthly, 0.000001)

	repeated, err := repo.ApplySubscriptionRefund(ctx, opID)
	require.NoError(t, err)
	require.Equal(t, result, repeated)
	daily, weekly, monthly = readSubscriptionUsage(t, fixture.subscriptionID)
	require.InDelta(t, 6, daily, 0.000001)
	require.InDelta(t, 16, weekly, 0.000001)
	require.InDelta(t, 26, monthly, 0.000001)
}

func TestAccountResetRefundBackfillsRetainedPreLedgerUsageOnDemand(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 5, 5, 5)
	requestID := fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 5, time.Now().UTC().Add(-time.Minute))
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM subscription_account_usage_ledger WHERE request_id=$1`, requestID)
	require.NoError(t, err)

	repo := NewAccountResetRepository(integrationDB)
	result, err := repo.ApplySubscriptionRefund(ctx, beginRefundOperation(t, repo, fixture.accountID))
	require.NoError(t, err)
	require.InDelta(t, 5, result.DailyRefundedUSD, 0.000001)
}

func TestAccountResetRefundIncludesSubscriptionThatBecameInactive(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 5, 5, 5)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 5, time.Now().UTC().Add(-time.Minute))
	repo := NewAccountResetRepository(integrationDB)
	opID := beginRefundOperation(t, repo, fixture.accountID)

	_, err := integrationDB.ExecContext(ctx, `UPDATE user_subscriptions
		SET status=$2, expires_at=NOW()-INTERVAL '1 hour', deleted_at=NOW()
		WHERE id=$1`, fixture.subscriptionID, service.SubscriptionStatusSuspended)
	require.NoError(t, err)

	result, err := repo.ApplySubscriptionRefund(ctx, opID)
	require.NoError(t, err)
	require.InDelta(t, 5, result.DailyRefundedUSD, 0.000001)
	daily, weekly, monthly := readSubscriptionUsage(t, fixture.subscriptionID)
	require.Zero(t, daily)
	require.Zero(t, weekly)
	require.Zero(t, monthly)
}

func TestAccountResetRefundFindsNewContributionAfterUsageLogCleanup(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 5, 5, 5)
	oldRequestID := fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 5, time.Now().UTC().Add(-time.Minute))
	repo := NewAccountResetRepository(integrationDB)

	firstID := beginRefundOperation(t, repo, fixture.accountID)
	first, err := repo.ApplySubscriptionRefund(ctx, firstID)
	require.NoError(t, err)
	require.InDelta(t, 5, first.DailyRefundedUSD, 0.000001)

	_, err = integrationDB.ExecContext(ctx, `DELETE FROM usage_logs WHERE request_id=$1`, oldRequestID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET daily_usage_usd=2,weekly_usage_usd=2,monthly_usage_usd=2 WHERE id=$1`, fixture.subscriptionID)
	require.NoError(t, err)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 2, time.Now().UTC())

	secondID := beginRefundOperation(t, repo, fixture.accountID)
	second, err := repo.ApplySubscriptionRefund(ctx, secondID)
	require.NoError(t, err)
	require.InDelta(t, 2, second.DailyRefundedUSD, 0.000001)
	require.InDelta(t, 2, second.WeeklyRefundedUSD, 0.000001)
	require.InDelta(t, 2, second.MonthlyRefundedUSD, 0.000001)
	daily, weekly, monthly := readSubscriptionUsage(t, fixture.subscriptionID)
	require.Zero(t, daily)
	require.Zero(t, weekly)
	require.Zero(t, monthly)
}

func TestAccountResetRefundUsesCurrentWindowAfterRollover(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 5, 5, 5)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 5, time.Now().UTC().Add(-time.Minute))
	repo := NewAccountResetRepository(integrationDB)

	firstID := beginRefundOperation(t, repo, fixture.accountID)
	_, err := repo.ApplySubscriptionRefund(ctx, firstID)
	require.NoError(t, err)

	newDayStart := time.Now().UTC()
	_, err = integrationDB.ExecContext(ctx, `UPDATE user_subscriptions SET daily_window_start=$2,daily_usage_usd=3,weekly_usage_usd=3,monthly_usage_usd=3 WHERE id=$1`, fixture.subscriptionID, newDayStart)
	require.NoError(t, err)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 3, newDayStart.Add(time.Microsecond))

	secondID := beginRefundOperation(t, repo, fixture.accountID)
	second, err := repo.ApplySubscriptionRefund(ctx, secondID)
	require.NoError(t, err)
	require.InDelta(t, 3, second.DailyRefundedUSD, 0.000001)
	daily, _, _ := readSubscriptionUsage(t, fixture.subscriptionID)
	require.Zero(t, daily)
}

func TestAccountResetRefundExcludesUsageAfterResetSucceeded(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 7, 7, 7)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 5, time.Now().UTC().Add(-time.Minute))
	repo := NewAccountResetRepository(integrationDB)
	opID := beginRefundOperation(t, repo, fixture.accountID)

	var cutoff time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT upstream_succeeded_at FROM account_reset_operations WHERE id=$1`, opID).Scan(&cutoff))
	afterCutoff := cutoff.Add(time.Microsecond)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 2, afterCutoff)

	result, err := repo.ApplySubscriptionRefund(ctx, opID)
	require.NoError(t, err)
	require.InDelta(t, 5, result.DailyRefundedUSD, 0.000001)
	daily, weekly, monthly := readSubscriptionUsage(t, fixture.subscriptionID)
	require.InDelta(t, 2, daily, 0.000001)
	require.InDelta(t, 2, weekly, 0.000001)
	require.InDelta(t, 2, monthly, 0.000001)

	var unsettled int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_account_usage_ledger WHERE account_id=$1 AND reset_operation_id IS NULL`, fixture.accountID).Scan(&unsettled))
	require.Equal(t, 1, unsettled)
}

func TestAccountResetAtomicLocalRetryPreservesUsageAfterReset(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 0, 0, 0)
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_build_object('quota_used',9,'quota_daily_used',9,'quota_weekly_used',9) WHERE id=$1`, fixture.accountID)
	require.NoError(t, err)
	repo := NewAccountResetRepository(integrationDB)
	op, created, err := repo.Begin(ctx, uuid.NewString(), fixture.accountID, service.AccountResetTypeLocalQuota, false, "", nil)
	require.NoError(t, err)
	require.True(t, created)

	require.NoError(t, repo.ResetLocalQuotaAndMarkSucceeded(ctx, op.ID, fixture.accountID))
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{quota_used}','2'::jsonb) WHERE id=$1`, fixture.accountID)
	require.NoError(t, err)
	require.NoError(t, repo.ResetLocalQuotaAndMarkSucceeded(ctx, op.ID, fixture.accountID))

	var quotaUsed float64
	var status string
	var cutoff sql.NullTime
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE((extra->>'quota_used')::numeric,0) FROM accounts WHERE id=$1`, fixture.accountID).Scan(&quotaUsed))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status,upstream_succeeded_at FROM account_reset_operations WHERE id=$1`, op.ID).Scan(&status, &cutoff))
	require.InDelta(t, 2, quotaUsed, 0.000001)
	require.Equal(t, "upstream_succeeded", status)
	require.True(t, cutoff.Valid)
	require.NoError(t, repo.CompleteWithoutRefund(ctx, op.ID))
}

func TestAccountResetBeginReplaysPersistedUpstreamResult(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 0, 0, 0)
	repo := NewAccountResetRepository(integrationDB)
	key := uuid.NewString()
	op, created, err := repo.Begin(ctx, key, fixture.accountID, service.AccountResetTypeOpenAICredit, false, uuid.NewString(), nil)
	require.NoError(t, err)
	require.True(t, created)
	expected := &service.OpenAIQuotaResetResult{Code: "ok", WindowsReset: 2}
	require.NoError(t, repo.MarkUpstreamSucceeded(ctx, op.ID, expected))

	replayed, created, err := repo.Begin(ctx, key, fixture.accountID, service.AccountResetTypeOpenAICredit, false, uuid.NewString(), nil)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, expected, replayed.UpstreamResult)
}

func TestAccountResetWorkerClaimsUpstreamSucceededAfterCrashWindow(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 0, 0, 0)
	repo := NewAccountResetRepository(integrationDB)
	op, created, err := repo.Begin(ctx, uuid.NewString(), fixture.accountID, service.AccountResetTypeOpenAICredit, false, uuid.NewString(), nil)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, repo.MarkUpstreamSucceeded(ctx, op.ID, &service.OpenAIQuotaResetResult{Code: "ok", WindowsReset: 1}))

	claimed, err := repo.ClaimLocalPending(ctx, "crash-recovery-worker", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, op.ID, claimed[0].ID)
	require.Equal(t, "processing_local", claimed[0].Status)
	require.NoError(t, repo.CompleteWithoutRefund(ctx, op.ID))
}

func TestAccountResetRefundConcurrentOperationsDoNotDoubleRefund(t *testing.T) {
	ctx := context.Background()
	fixture := newAccountResetRefundFixture(t, 5, 5, 5)
	fixture.addUsage(t, fixture.accountID, service.BillingTypeSubscription, 5, time.Now().UTC().Add(-time.Minute))
	repo := NewAccountResetRepository(integrationDB)
	operationIDs := []int64{
		beginRefundOperation(t, repo, fixture.accountID),
		beginRefundOperation(t, repo, fixture.accountID),
	}

	type outcome struct {
		result *service.AccountResetRefundResult
		err    error
	}
	outcomes := make(chan outcome, len(operationIDs))
	var wg sync.WaitGroup
	for _, operationID := range operationIDs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			var result *service.AccountResetRefundResult
			var err error
			for attempt := 0; attempt < 5; attempt++ {
				result, err = repo.ApplySubscriptionRefund(ctx, id)
				if err == nil || !isSerializationFailure(err) {
					break
				}
			}
			outcomes <- outcome{result: result, err: err}
		}(operationID)
	}
	wg.Wait()
	close(outcomes)

	var totalRefunded float64
	for item := range outcomes {
		require.NoError(t, item.err)
		totalRefunded += item.result.DailyRefundedUSD
	}
	require.InDelta(t, 5, totalRefunded, 0.000001)
	daily, weekly, monthly := readSubscriptionUsage(t, fixture.subscriptionID)
	require.Zero(t, daily)
	require.Zero(t, weekly)
	require.Zero(t, monthly)

	var adjustmentCount int
	var observedDaily float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(observed_daily_contribution_usd),0) FROM account_reset_subscription_adjustments WHERE operation_id=ANY($1)`, pq.Array(operationIDs)).Scan(&adjustmentCount, &observedDaily))
	require.Equal(t, 1, adjustmentCount)
	require.InDelta(t, 5, observedDaily, 0.000001)
}

func isSerializationFailure(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "40001"
}
