//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quotaStatusHandlerSettingRepo struct {
	value string
}

func (r *quotaStatusHandlerSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (r *quotaStatusHandlerSettingRepo) GetValue(_ context.Context, _ string) (string, error) {
	return r.value, nil
}

func (r *quotaStatusHandlerSettingRepo) Set(_ context.Context, _, value string) error {
	r.value = value
	return nil
}

func (r *quotaStatusHandlerSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (r *quotaStatusHandlerSettingRepo) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (r *quotaStatusHandlerSettingRepo) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (r *quotaStatusHandlerSettingRepo) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

type quotaStatusHandlerAdminStub struct {
	service.AdminService
	groups   map[int64]*service.Group
	accounts map[int64]*service.Account
}

func (s *quotaStatusHandlerAdminStub) GetGroup(_ context.Context, id int64) (*service.Group, error) {
	return s.groups[id], nil
}

func (s *quotaStatusHandlerAdminStub) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	accounts := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		if account := s.accounts[id]; account != nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

type quotaStatusGroupAccessStub struct {
	groups []service.Group
}

func (s *quotaStatusGroupAccessStub) GetAvailableGroups(context.Context, int64) ([]service.Group, error) {
	return s.groups, nil
}

func newQuotaStatusAccessHandler(t *testing.T, accessMode string, access quotaStatusGroupAccess) *QuotaStatusHandler {
	t.Helper()
	config := service.QuotaStatusConfig{
		Enabled:    true,
		AccessMode: accessMode,
		Groups: []service.QuotaStatusGroupConfig{
			{GroupID: 7, DisplayName: "Seven", Accounts: []service.QuotaStatusAccountConfig{{AccountID: 11}}},
			{GroupID: 8, DisplayName: "Eight", Accounts: []service.QuotaStatusAccountConfig{{AccountID: 12}}},
		},
	}
	payload, err := json.Marshal(config)
	require.NoError(t, err)
	admin := &quotaStatusHandlerAdminStub{
		groups: map[int64]*service.Group{
			7: {ID: 7, Name: "Seven", Platform: service.PlatformOpenAI},
			8: {ID: 8, Name: "Eight", Platform: service.PlatformOpenAI},
		},
		accounts: map[int64]*service.Account{
			11: {ID: 11, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{7}},
			12: {ID: 12, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{8}},
		},
	}
	quotaService := service.NewQuotaStatusService(
		admin,
		service.NewSettingService(&quotaStatusHandlerSettingRepo{value: string(payload)}, nil),
		nil,
	)
	return &QuotaStatusHandler{service: quotaService, groupAccess: access}
}

func serveQuotaStatus(t *testing.T, handler *QuotaStatusHandler, userID int64, role string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if userID > 0 {
		router.Use(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: userID})
			c.Set(string(servermiddleware.ContextKeyUserRole), role)
			c.Next()
		})
	}
	router.GET("/quota-status", handler.GetPublic)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/quota-status", nil))
	return recorder
}

func decodeQuotaStatusSnapshot(t *testing.T, recorder *httptest.ResponseRecorder) service.QuotaStatusSnapshot {
	t.Helper()
	var envelope struct {
		Data service.QuotaStatusSnapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

func TestQuotaStatusAuthenticatedModeRejectsAnonymousViewer(t *testing.T) {
	handler := newQuotaStatusAccessHandler(t, service.QuotaStatusAccessModeAuthenticated, nil)
	recorder := serveQuotaStatus(t, handler, 0, "")
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestQuotaStatusGroupScopedModeFiltersRegularUserGroups(t *testing.T) {
	handler := newQuotaStatusAccessHandler(t, service.QuotaStatusAccessModeGroupScoped, &quotaStatusGroupAccessStub{
		groups: []service.Group{{ID: 8}},
	})
	recorder := serveQuotaStatus(t, handler, 42, service.RoleUser)
	require.Equal(t, http.StatusOK, recorder.Code)
	snapshot := decodeQuotaStatusSnapshot(t, recorder)
	require.Len(t, snapshot.Groups, 1)
	require.Equal(t, "Eight", snapshot.Groups[0].Name)
}

func TestQuotaStatusGroupScopedModeLetsAdminSeeAllConfiguredGroups(t *testing.T) {
	handler := newQuotaStatusAccessHandler(t, service.QuotaStatusAccessModeGroupScoped, nil)
	recorder := serveQuotaStatus(t, handler, 1, service.RoleAdmin)
	require.Equal(t, http.StatusOK, recorder.Code)
	snapshot := decodeQuotaStatusSnapshot(t, recorder)
	require.Len(t, snapshot.Groups, 2)
}
