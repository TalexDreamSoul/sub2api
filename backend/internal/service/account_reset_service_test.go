//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountResetTestRepo struct {
	op           AccountResetOperation
	applyErr     error
	resetErr     error
	resetCalls   int
	localPending bool
}

func (r *accountResetTestRepo) Begin(_ context.Context, key string, accountID int64, resetType string, restore bool, redeem string, actor *int64) (*AccountResetOperation, bool, error) {
	if r.op.ID == 0 {
		r.op = AccountResetOperation{ID: 1, OperationKey: key, AccountID: accountID, ResetType: resetType, Status: "pending", RestoreSubscriptionUsage: restore, UpstreamRedeemRequestID: redeem, CreatedBy: actor}
		return &r.op, true, nil
	}
	copy := r.op
	return &copy, false, nil
}
func (r *accountResetTestRepo) ResetLocalQuotaAndMarkSucceeded(context.Context, int64, int64) error {
	r.resetCalls++
	if r.resetErr != nil {
		err := r.resetErr
		r.resetErr = nil
		return err
	}
	r.op.Status = "upstream_succeeded"
	return nil
}
func (r *accountResetTestRepo) MarkUpstreamSucceeded(_ context.Context, _ int64, result *OpenAIQuotaResetResult) error {
	r.op.Status = "upstream_succeeded"
	if result != nil {
		copy := *result
		r.op.UpstreamResult = &copy
	}
	return nil
}
func (r *accountResetTestRepo) MarkLocalPending(context.Context, int64, string) error {
	r.op.Status = "local_pending"
	r.localPending = true
	return nil
}
func (r *accountResetTestRepo) MarkFailed(context.Context, int64, string) error {
	r.op.Status = "failed"
	return nil
}
func (r *accountResetTestRepo) CompleteWithoutRefund(context.Context, int64) error {
	r.op.Status = "completed"
	return nil
}
func (r *accountResetTestRepo) ApplySubscriptionRefund(context.Context, int64) (*AccountResetRefundResult, error) {
	if r.applyErr != nil {
		return nil, r.applyErr
	}
	r.op.Status = "completed"
	return &AccountResetRefundResult{Adjustments: []AccountResetAdjustment{}}, nil
}
func (r *accountResetTestRepo) ClaimLocalPending(context.Context, string, int, time.Duration) ([]AccountResetOperation, error) {
	return nil, nil
}

type accountQuotaResetterTest struct {
	account *Account
}

func (r *accountQuotaResetterTest) GetByID(context.Context, int64) (*Account, error) {
	if r.account != nil {
		return r.account, nil
	}
	return &Account{}, nil
}

func TestAccountResetReusesUpstreamRedeemIDAfterAmbiguousFailure(t *testing.T) {
	repo := &accountResetTestRepo{}
	svc := &AccountResetService{repo: repo, accountRepo: &accountQuotaResetterTest{}}
	request := AccountResetRequest{AccountID: 8, ActorID: 2, IdempotencyKey: "same-key"}
	var ids []string
	callback := func(_ context.Context, id string) (*OpenAIQuotaResetResult, error) {
		ids = append(ids, id)
		if len(ids) == 1 {
			return nil, errors.New("ambiguous network error")
		}
		return &OpenAIQuotaResetResult{Code: "ok"}, nil
	}
	_, _, err := svc.ExecuteOpenAICreditReset(context.Background(), request, callback)
	require.Error(t, err)
	_, operation, err := svc.ExecuteOpenAICreditReset(context.Background(), request, callback)
	require.NoError(t, err)
	require.Len(t, ids, 2)
	require.Equal(t, ids[0], ids[1])
	require.NotEmpty(t, ids[0])
	require.Equal(t, "completed", operation.Status)
}

func TestAccountResetReplaysPersistedUpstreamResult(t *testing.T) {
	repo := &accountResetTestRepo{}
	svc := &AccountResetService{repo: repo, accountRepo: &accountQuotaResetterTest{}}
	request := AccountResetRequest{AccountID: 8, ActorID: 2, IdempotencyKey: "same-key"}
	calls := 0
	callback := func(context.Context, string) (*OpenAIQuotaResetResult, error) {
		calls++
		return &OpenAIQuotaResetResult{Code: "ok", WindowsReset: 2}, nil
	}

	first, _, err := svc.ExecuteOpenAICreditReset(context.Background(), request, callback)
	require.NoError(t, err)
	replayed, operation, err := svc.ExecuteOpenAICreditReset(context.Background(), request, callback)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, first, replayed)
	require.Equal(t, "completed", operation.Status)
}

func TestAccountResetRetriesAtomicLocalResetWithSameOperation(t *testing.T) {
	repo := &accountResetTestRepo{resetErr: errors.New("ambiguous database error")}
	accountRepo := &accountQuotaResetterTest{}
	svc := &AccountResetService{repo: repo, accountRepo: accountRepo}
	request := AccountResetRequest{AccountID: 8, ActorID: 2, IdempotencyKey: "same-key"}
	_, err := svc.ExecuteLocalQuotaReset(context.Background(), request)
	require.Error(t, err)
	result, err := svc.ExecuteLocalQuotaReset(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "completed", result.Status)
	require.Equal(t, 2, repo.resetCalls)
}

func TestAccountResetRejectsCredentialShadow(t *testing.T) {
	parentID := int64(1)
	accountRepo := &accountQuotaResetterTest{account: &Account{ParentAccountID: &parentID}}
	repo := &accountResetTestRepo{}
	svc := &AccountResetService{repo: repo, accountRepo: accountRepo}
	_, err := svc.ExecuteLocalQuotaReset(context.Background(), AccountResetRequest{AccountID: 8, ActorID: 2, IdempotencyKey: "key"})
	require.Error(t, err)
	require.Zero(t, repo.resetCalls)
}

func TestAccountResetRejectsSubscriptionUsageRefund(t *testing.T) {
	repo := &accountResetTestRepo{}
	svc := &AccountResetService{repo: repo, accountRepo: &accountQuotaResetterTest{}}

	_, err := svc.ExecuteLocalQuotaReset(context.Background(), AccountResetRequest{
		AccountID: 8, ActorID: 2, IdempotencyKey: "key", RestoreSubscriptionUsage: true,
	})
	require.Error(t, err)
	require.Zero(t, repo.op.ID)
}

func TestAccountResetReturnsLocalPendingAfterIrreversibleReset(t *testing.T) {
	repo := &accountResetTestRepo{applyErr: errors.New("database unavailable")}
	svc := &AccountResetService{repo: repo, accountRepo: &accountQuotaResetterTest{}}
	_, operation, err := svc.ExecuteOpenAICreditReset(context.Background(), AccountResetRequest{AccountID: 8, ActorID: 2, IdempotencyKey: "key"}, func(context.Context, string) (*OpenAIQuotaResetResult, error) {
		return &OpenAIQuotaResetResult{Code: "ok"}, nil
	})
	require.NoError(t, err)
	require.True(t, repo.localPending)
	require.Equal(t, "local_pending", operation.Status)
}
