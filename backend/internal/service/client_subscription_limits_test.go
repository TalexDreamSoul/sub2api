//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type clientSubscriptionLimitsServiceAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r *clientSubscriptionLimitsServiceAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, _ string) ([]Account, error) {
	return r.accounts, nil
}

type clientSubscriptionLimitsQuotaReader struct {
	usageByAccountID map[int64]*OpenAIQuotaUsage
}

func (r *clientSubscriptionLimitsQuotaReader) QueryUsage(_ context.Context, accountID int64) (*OpenAIQuotaUsage, error) {
	usage, ok := r.usageByAccountID[accountID]
	if !ok {
		return nil, errors.New("quota lookup for a non-OAuth account")
	}
	return usage, nil
}

func TestClientSubscriptionLimitsServiceAggregatesWindowIDAcrossOAuthAccountPool(t *testing.T) {
	repo := &clientSubscriptionLimitsServiceAccountRepo{accounts: []Account{
		{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 103, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}}
	quotaReader := &clientSubscriptionLimitsQuotaReader{usageByAccountID: map[int64]*OpenAIQuotaUsage{
		101: {RateLimit: &OpenAIRateLimit{
			PrimaryWindow:   &OpenAIRateLimitWindow{UsedPercent: 20, LimitWindowSeconds: int64(RateLimitWindow5h.Seconds()), ResetAt: 1767225900},
			SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 40, LimitWindowSeconds: int64(RateLimitWindow7d.Seconds()), ResetAt: 1767830400},
		}},
		102: {RateLimit: &OpenAIRateLimit{
			PrimaryWindow:   &OpenAIRateLimitWindow{UsedPercent: 60, LimitWindowSeconds: int64(RateLimitWindow5h.Seconds()), ResetAt: 1767225600},
			SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 80, LimitWindowSeconds: int64(RateLimitWindow7d.Seconds()), ResetAt: 1767744000},
		}},
	}}
	service := &ClientSubscriptionLimitsService{accountRepo: repo, quotaService: quotaReader}

	response, err := service.Get(context.Background(), &Group{ID: 9, Platform: PlatformOpenAI})

	require.NoError(t, err)
	require.Equal(t, ClientSubscriptionLimitsProvider, response.Provider)
	require.Equal(t, ClientSubscriptionLimitsScope, response.Scope)
	require.Equal(t, []ClientSubscriptionLimitWindow{
		{ID: "1w", UsedPercent: 60, ResetAt: "2026-01-07T00:00:00Z"},
		{ID: "5h", UsedPercent: 40, ResetAt: "2026-01-01T00:00:00Z"},
	}, response.Windows)
}
