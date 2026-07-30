//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaStatusAdminStub struct {
	AdminService
	groups   map[int64]*Group
	accounts map[int64]*Account
}

func (s *quotaStatusAdminStub) GetGroup(_ context.Context, id int64) (*Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func (s *quotaStatusAdminStub) GetAccountsByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	accounts := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account := s.accounts[id]; account != nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

type quotaStatusSettingRepo struct {
	value string
}

func (r *quotaStatusSettingRepo) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (r *quotaStatusSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyQuotaStatusConfig || r.value == "" {
		return "", ErrSettingNotFound
	}
	return r.value, nil
}

func (r *quotaStatusSettingRepo) Set(_ context.Context, key, value string) error {
	if key == SettingKeyQuotaStatusConfig {
		r.value = value
	}
	return nil
}

func (r *quotaStatusSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (r *quotaStatusSettingRepo) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (r *quotaStatusSettingRepo) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (r *quotaStatusSettingRepo) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestQuotaStatusConfigDefaultsMissingAccessModeToPublic(t *testing.T) {
	repo := &quotaStatusSettingRepo{value: `{"enabled":true,"title":"容量状态","groups":[]}`}
	quotaService := NewQuotaStatusService(
		&quotaStatusAdminStub{groups: map[int64]*Group{}, accounts: map[int64]*Account{}},
		NewSettingService(repo, nil),
		nil,
	)

	config, err := quotaService.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, QuotaStatusAccessModePublic, config.AccessMode)
}

func TestQuotaStatusConfigUnknownAccessModeFailsClosed(t *testing.T) {
	repo := &quotaStatusSettingRepo{value: `{"enabled":true,"access_mode":"typo","groups":[]}`}
	quotaService := NewQuotaStatusService(
		&quotaStatusAdminStub{groups: map[int64]*Group{}, accounts: map[int64]*Account{}},
		NewSettingService(repo, nil),
		nil,
	)

	config, err := quotaService.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, QuotaStatusAccessModeAuthenticated, config.AccessMode)
}

func TestQuotaStatusSnapshotFiltersConfiguredGroupsByViewerAccess(t *testing.T) {
	accounts := map[int64]*Account{
		11: {ID: 11, Name: "group-seven", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{7}},
		12: {ID: 12, Name: "group-eight", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{8}},
	}
	config := QuotaStatusConfig{
		Enabled:    true,
		AccessMode: QuotaStatusAccessModeGroupScoped,
		Groups: []QuotaStatusGroupConfig{
			{GroupID: 7, DisplayName: "Seven", Accounts: []QuotaStatusAccountConfig{{AccountID: 11}}},
			{GroupID: 8, DisplayName: "Eight", Accounts: []QuotaStatusAccountConfig{{AccountID: 12}}},
		},
	}
	payload, err := json.Marshal(config)
	require.NoError(t, err)

	quotaService := NewQuotaStatusService(
		&quotaStatusAdminStub{
			groups: map[int64]*Group{
				7: {ID: 7, Name: "Seven", Platform: PlatformOpenAI},
				8: {ID: 8, Name: "Eight", Platform: PlatformOpenAI},
			},
			accounts: accounts,
		},
		NewSettingService(&quotaStatusSettingRepo{value: string(payload)}, nil),
		nil,
	)

	snapshot, err := quotaService.GetSnapshotForGroups(context.Background(), map[int64]struct{}{8: {}})
	require.NoError(t, err)
	require.Equal(t, QuotaStatusAccessModeGroupScoped, snapshot.AccessMode)
	require.Len(t, snapshot.Groups, 1)
	require.Equal(t, "Eight", snapshot.Groups[0].Name)
}

func TestQuotaStatusSnapshotHidesAccountNameAndReusesQuotaDetails(t *testing.T) {
	limit := 100.0
	used := 72.5
	account := &Account{
		ID:          11,
		Name:        "codex-primary@example.com",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		GroupIDs:    []int64{7},
		Extra: map[string]any{
			"quota_limit": limit,
			"quota_used":  used,
		},
	}
	config := QuotaStatusConfig{
		Enabled: true,
		Title:   "容量状态",
		Groups: []QuotaStatusGroupConfig{{
			GroupID: 7,
			Accounts: []QuotaStatusAccountConfig{{
				AccountID: account.ID,
				ShowName:  false,
			}},
		}},
	}
	payload, err := json.Marshal(config)
	require.NoError(t, err)

	repo := &quotaStatusSettingRepo{value: string(payload)}
	service := NewQuotaStatusService(
		&quotaStatusAdminStub{
			groups:   map[int64]*Group{7: {ID: 7, Name: "OpenAI", Platform: PlatformOpenAI}},
			accounts: map[int64]*Account{account.ID: account},
		},
		NewSettingService(repo, nil),
		nil,
	)

	snapshot, err := service.GetSnapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.Groups, 1)
	require.Len(t, snapshot.Groups[0].Accounts, 1)
	publicAccount := snapshot.Groups[0].Accounts[0]
	require.Equal(t, "账号 1", publicAccount.Name)
	require.NotContains(t, publicAccount.Name, "example.com")
	require.Equal(t, "available", publicAccount.Status)
	require.Len(t, publicAccount.Dimensions, 1)
	require.Equal(t, "total", publicAccount.Dimensions[0].Key)
	require.InDelta(t, used, *publicAccount.Dimensions[0].Used, 0.001)
	require.InDelta(t, limit, *publicAccount.Dimensions[0].Limit, 0.001)
}

func TestAccountUsageServiceBuildsPassiveSnapshotsForPersistedPlatforms(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	usageService := &AccountUsageService{grokQuotaFetcher: NewGrokQuotaFetcher()}

	openAIUsage, err := usageService.GetPassiveUsageForAccount(context.Background(), &Account{
		ID:       21,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent":  42.5,
			"codex_5h_reset_at":      resetAt,
			"codex_7d_used_percent":  73.0,
			"codex_7d_reset_at":      resetAt,
			"codex_usage_updated_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "passive", openAIUsage.Source)
	require.NotNil(t, openAIUsage.FiveHour)
	require.InDelta(t, 42.5, openAIUsage.FiveHour.Utilization, 0.001)
	require.NotNil(t, openAIUsage.SevenDay)
	require.InDelta(t, 73.0, openAIUsage.SevenDay.Utilization, 0.001)

	grokUsage, err := usageService.GetPassiveUsageForAccount(context.Background(), &Account{
		ID:       22,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			grokQuotaSnapshotExtraKey: map[string]any{
				"requests":   map[string]any{"limit": 100, "remaining": 25},
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "passive", grokUsage.Source)
	require.NotNil(t, grokUsage.GrokRequestQuota)
	require.EqualValues(t, 100, *grokUsage.GrokRequestQuota.Limit)
	require.EqualValues(t, 25, *grokUsage.GrokRequestQuota.Remaining)
}

func TestPublicAccountStatusDistinguishesLimitedAndUnavailable(t *testing.T) {
	future := time.Now().Add(time.Hour)
	limited := &Account{Status: StatusActive, Schedulable: true, RateLimitResetAt: &future}
	require.Equal(t, "limited", publicAccountStatus(limited))

	exhausted := &Account{
		Status:      StatusActive,
		Schedulable: true,
		Type:        AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_limit": 10.0,
			"quota_used":  10.0,
		},
	}
	require.Equal(t, "unavailable", publicAccountStatus(exhausted))
}
