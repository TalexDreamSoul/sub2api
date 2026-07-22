package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type contentModerationInternalAuditUserRepo struct {
	users  map[string]*User
	nextID int64
}

func (r *contentModerationInternalAuditUserRepo) Create(ctx context.Context, user *User) error {
	if r.users == nil {
		r.users = map[string]*User{}
	}
	key := strings.ToLower(strings.TrimSpace(user.Email))
	if _, ok := r.users[key]; ok {
		return ErrEmailExists
	}
	r.nextID++
	clone := *user
	clone.ID = r.nextID
	r.users[key] = &clone
	user.ID = clone.ID
	return nil
}

func (r *contentModerationInternalAuditUserRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	for _, user := range r.users {
		if user.ID == id {
			clone := *user
			return &clone, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *contentModerationInternalAuditUserRepo) GetByIDIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	return r.GetByID(ctx, id)
}

func (r *contentModerationInternalAuditUserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	if r.users == nil {
		return nil, ErrUserNotFound
	}
	user, ok := r.users[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *user
	return &clone, nil
}

func (r *contentModerationInternalAuditUserRepo) GetFirstAdmin(ctx context.Context) (*User, error) {
	panic("unexpected GetFirstAdmin call")
}

func (r *contentModerationInternalAuditUserRepo) Update(ctx context.Context, user *User) error {
	if r.users == nil {
		r.users = map[string]*User{}
	}
	clone := *user
	r.users[strings.ToLower(strings.TrimSpace(clone.Email))] = &clone
	return nil
}

func (r *contentModerationInternalAuditUserRepo) BatchUpdateLimits(context.Context, []int64, *int, *int) (int, error) {
	panic("unexpected BatchUpdateLimits call")
}

func (r *contentModerationInternalAuditUserRepo) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (r *contentModerationInternalAuditUserRepo) GetUserAvatar(context.Context, int64) (*UserAvatar, error) {
	panic("unexpected GetUserAvatar call")
}
func (r *contentModerationInternalAuditUserRepo) UpsertUserAvatar(context.Context, int64, UpsertUserAvatarInput) (*UserAvatar, error) {
	panic("unexpected UpsertUserAvatar call")
}
func (r *contentModerationInternalAuditUserRepo) DeleteUserAvatar(context.Context, int64) error {
	panic("unexpected DeleteUserAvatar call")
}
func (r *contentModerationInternalAuditUserRepo) List(context.Context, pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (r *contentModerationInternalAuditUserRepo) ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (r *contentModerationInternalAuditUserRepo) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserIDs call")
}
func (r *contentModerationInternalAuditUserRepo) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserID call")
}
func (r *contentModerationInternalAuditUserRepo) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	panic("unexpected UpdateUserLastActiveAt call")
}
func (r *contentModerationInternalAuditUserRepo) UpdateBalance(context.Context, int64, float64) error {
	panic("unexpected UpdateBalance call")
}
func (r *contentModerationInternalAuditUserRepo) DeductBalance(context.Context, int64, float64) error {
	panic("unexpected DeductBalance call")
}
func (r *contentModerationInternalAuditUserRepo) UpdateConcurrency(context.Context, int64, int) error {
	panic("unexpected UpdateConcurrency call")
}
func (r *contentModerationInternalAuditUserRepo) BatchSetConcurrency(context.Context, []int64, int) (int, error) {
	panic("unexpected BatchSetConcurrency call")
}
func (r *contentModerationInternalAuditUserRepo) BatchAddConcurrency(context.Context, []int64, int) (int, error) {
	panic("unexpected BatchAddConcurrency call")
}
func (r *contentModerationInternalAuditUserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	_, err := r.GetByEmail(ctx, email)
	return err == nil, nil
}
func (r *contentModerationInternalAuditUserRepo) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	panic("unexpected RemoveGroupFromAllowedGroups call")
}
func (r *contentModerationInternalAuditUserRepo) AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error {
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !int64SliceContains(user.AllowedGroups, groupID) {
		user.AllowedGroups = append(user.AllowedGroups, groupID)
	}
	return r.Update(ctx, user)
}
func (r *contentModerationInternalAuditUserRepo) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	panic("unexpected RemoveGroupFromUserAllowedGroups call")
}
func (r *contentModerationInternalAuditUserRepo) ListUserAuthIdentities(context.Context, int64) ([]UserAuthIdentityRecord, error) {
	panic("unexpected ListUserAuthIdentities call")
}
func (r *contentModerationInternalAuditUserRepo) UnbindUserAuthProvider(context.Context, int64, string) error {
	panic("unexpected UnbindUserAuthProvider call")
}
func (r *contentModerationInternalAuditUserRepo) UpdateTotpSecret(context.Context, int64, *string) error {
	panic("unexpected UpdateTotpSecret call")
}
func (r *contentModerationInternalAuditUserRepo) EnableTotp(context.Context, int64) error {
	panic("unexpected EnableTotp call")
}
func (r *contentModerationInternalAuditUserRepo) DisableTotp(context.Context, int64) error {
	panic("unexpected DisableTotp call")
}

type contentModerationInternalAuditGroupRepo struct {
	groups map[int64]*Group
}

func (r *contentModerationInternalAuditGroupRepo) Create(context.Context, *Group) error {
	panic("unexpected Create call")
}
func (r *contentModerationInternalAuditGroupRepo) GetByID(ctx context.Context, id int64) (*Group, error) {
	group, ok := r.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	clone := *group
	return &clone, nil
}
func (r *contentModerationInternalAuditGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return r.GetByID(ctx, id)
}
func (r *contentModerationInternalAuditGroupRepo) Update(context.Context, *Group) error {
	panic("unexpected Update call")
}
func (r *contentModerationInternalAuditGroupRepo) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (r *contentModerationInternalAuditGroupRepo) DeleteCascade(context.Context, int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (r *contentModerationInternalAuditGroupRepo) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (r *contentModerationInternalAuditGroupRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (r *contentModerationInternalAuditGroupRepo) ListActive(context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}
func (r *contentModerationInternalAuditGroupRepo) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (r *contentModerationInternalAuditGroupRepo) ExistsByName(context.Context, string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (r *contentModerationInternalAuditGroupRepo) GetAccountCount(context.Context, int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}
func (r *contentModerationInternalAuditGroupRepo) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID call")
}
func (r *contentModerationInternalAuditGroupRepo) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs call")
}
func (r *contentModerationInternalAuditGroupRepo) BindAccountsToGroup(context.Context, int64, []int64) error {
	panic("unexpected BindAccountsToGroup call")
}
func (r *contentModerationInternalAuditGroupRepo) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders call")
}

type contentModerationInternalAuditAPIKeyRepo struct {
	keys   map[int64]*APIKey
	nextID int64
}

func (r *contentModerationInternalAuditAPIKeyRepo) Create(ctx context.Context, key *APIKey) error {
	if r.keys == nil {
		r.keys = map[int64]*APIKey{}
	}
	r.nextID++
	clone := *key
	clone.ID = r.nextID
	r.keys[clone.ID] = &clone
	key.ID = clone.ID
	return nil
}
func (r *contentModerationInternalAuditAPIKeyRepo) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	key, ok := r.keys[id]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	clone := *key
	return &clone, nil
}
func (r *contentModerationInternalAuditAPIKeyRepo) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	key, err := r.GetByID(ctx, id)
	if err != nil {
		return "", 0, err
	}
	return key.Key, key.UserID, nil
}
func (r *contentModerationInternalAuditAPIKeyRepo) GetByKey(ctx context.Context, key string) (*APIKey, error) {
	for _, item := range r.keys {
		if item.Key == key {
			clone := *item
			return &clone, nil
		}
	}
	return nil, ErrAPIKeyNotFound
}
func (r *contentModerationInternalAuditAPIKeyRepo) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	return r.GetByKey(ctx, key)
}
func (r *contentModerationInternalAuditAPIKeyRepo) Update(ctx context.Context, key *APIKey) error {
	clone := *key
	r.keys[clone.ID] = &clone
	return nil
}
func (r *contentModerationInternalAuditAPIKeyRepo) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) DeleteWithAudit(context.Context, int64) error {
	panic("unexpected DeleteWithAudit call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) ListByUserID(ctx context.Context, userID int64, params pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	out := make([]APIKey, 0)
	for _, key := range r.keys {
		if key.UserID == userID {
			out = append(out, *key)
		}
	}
	return out, &pagination.PaginationResult{Total: int64(len(out))}, nil
}
func (r *contentModerationInternalAuditAPIKeyRepo) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) ExistsByKey(ctx context.Context, key string) (bool, error) {
	_, err := r.GetByKey(ctx, key)
	return err == nil, nil
}
func (r *contentModerationInternalAuditAPIKeyRepo) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows call")
}
func (r *contentModerationInternalAuditAPIKeyRepo) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

func TestContentModerationUpdateConfig_InternalGroupCreatesInternalAPIKey(t *testing.T) {
	groupID := int64(42)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{}}
	userRepo := &contentModerationInternalAuditUserRepo{}
	groupRepo := &contentModerationInternalAuditGroupRepo{groups: map[int64]*Group{
		groupID: {ID: groupID, Name: "audit-group", Status: StatusActive, Platform: PlatformOpenAI},
	}}
	apiKeyRepo := &contentModerationInternalAuditAPIKeyRepo{}
	svc := NewContentModerationService(settingRepo, nil, nil, groupRepo, userRepo, nil, nil)
	svc.SetAPIKeyRepository(apiKeyRepo)

	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		AuditModels: &[]ContentModerationAuditModelConfig{{
			ID:       "audit-1",
			Name:     "Audit 1",
			Enabled:  true,
			Protocol: ContentModerationAuditProtocolInternalGroup,
			GroupID:  &groupID,
			Model:    "nex-agi/Nex-N2-Pro",
		}},
	})
	require.NoError(t, err)

	raw := settingRepo.values[SettingKeyContentModerationConfig]
	var cfg ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	require.Len(t, cfg.AuditModels, 1)
	require.Equal(t, ContentModerationAuditProtocolInternalGroup, cfg.AuditModels[0].Protocol)
	require.Equal(t, "audit-group", cfg.AuditModels[0].GroupName)
	require.NotNil(t, cfg.AuditModels[0].InternalAPIKeyID)
	require.Empty(t, cfg.AuditModels[0].APIKey)
	require.Empty(t, cfg.AuditModels[0].BaseURL)
	require.Len(t, apiKeyRepo.keys, 1)
	for _, key := range apiKeyRepo.keys {
		require.Equal(t, internalAuditAPIKeyName(groupID), key.Name)
		require.Equal(t, groupID, derefInt64(key.GroupID))
	}
	user, err := userRepo.GetByEmail(context.Background(), contentModerationInternalAuditUserEmail)
	require.NoError(t, err)
	require.Contains(t, user.AllowedGroups, groupID)
	require.GreaterOrEqual(t, user.Balance, contentModerationInternalAuditBalance)
}

func TestContentModerationUpdateConfig_InternalGroupRejectsInactiveGroup(t *testing.T) {
	groupID := int64(7)
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{}},
		nil,
		nil,
		&contentModerationInternalAuditGroupRepo{groups: map[int64]*Group{
			groupID: {ID: groupID, Name: "inactive", Status: StatusDisabled},
		}},
		&contentModerationInternalAuditUserRepo{},
		nil,
		nil,
	)
	svc.SetAPIKeyRepository(&contentModerationInternalAuditAPIKeyRepo{})

	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		AuditModels: &[]ContentModerationAuditModelConfig{{
			ID:       "audit-1",
			Enabled:  true,
			Protocol: ContentModerationAuditProtocolInternalGroup,
			GroupID:  &groupID,
			Model:    "model-a",
		}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "系统分组未启用")
}

func TestContentModerationAuditModelErrorMessageMapsChannelFailures(t *testing.T) {
	message := contentModerationAuditModelErrorMessage(http.StatusServiceUnavailable, `{"error":{"code":"model_not_found","message":"No available channel for model nex under group default"}}`)
	require.Equal(t, "所选分组没有可用该模型，请更换模型或补充渠道", message)

	message = contentModerationAuditModelErrorMessage(http.StatusInternalServerError, `upstream error`)
	require.Equal(t, "审核模型上游请求失败，请检查分组渠道和账号健康", message)
}

func TestContentModerationAuditModelErrorMessageExplainsInternalAccessBlock(t *testing.T) {
	body := `{"code":"SUBSCRIPTION_NOT_FOUND","message":"No active subscription found for this group"}`

	require.True(t, contentModerationAuditModelIsInternalAccessBlocked(body))
	require.Equal(
		t,
		"系统分组审计模型被订阅/分组权限拦截：内部审计账号应跳过订阅计费限制，请检查认证中间件配置",
		contentModerationAuditModelErrorMessage(http.StatusForbidden, body),
	)
}

func TestContentModerationAuditModelErrorDetailExplainsCanceledContext(t *testing.T) {
	groupID := int64(42)
	model := ContentModerationAuditModelConfig{
		Protocol:  ContentModerationAuditProtocolInternalGroup,
		Model:     "gpt-5.4-mini",
		GroupID:   &groupID,
		GroupName: "audit-group",
	}

	detail := contentModerationAuditModelErrorDetail(model, 0, fmt.Errorf("get internal audit group: %w", context.Canceled))
	require.Contains(t, detail, "系统分组审计模型")
	require.Contains(t, detail, "audit-group(42)")
	require.Contains(t, detail, "请求上下文已取消")
}
