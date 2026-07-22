package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type privilegedGuardQuotaRepo struct {
	service.UserPlatformQuotaRepository
}

func setupAdminRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	userHandler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	groupHandler := NewGroupHandler(adminSvc, nil, nil)
	proxyHandler := NewProxyHandler(adminSvc)
	redeemHandler := NewRedeemHandler(adminSvc, nil)

	router.GET("/api/v1/admin/users", userHandler.List)
	router.GET("/api/v1/admin/users/:id", userHandler.GetByID)
	router.POST("/api/v1/admin/users/:id/auth-identities", userHandler.BindAuthIdentity)
	router.POST("/api/v1/admin/users", userHandler.Create)
	router.PUT("/api/v1/admin/users/:id", userHandler.Update)
	router.DELETE("/api/v1/admin/users/:id", userHandler.Delete)
	router.POST("/api/v1/admin/users/:id/balance", userHandler.UpdateBalance)
	router.GET("/api/v1/admin/users/:id/api-keys", userHandler.GetUserAPIKeys)
	router.GET("/api/v1/admin/users/:id/usage", userHandler.GetUserUsage)

	router.GET("/api/v1/admin/groups", groupHandler.List)
	router.GET("/api/v1/admin/groups/all", groupHandler.GetAll)
	router.GET("/api/v1/admin/groups/:id/models-list-candidates", groupHandler.GetModelsListCandidates)
	router.GET("/api/v1/admin/groups/:id", groupHandler.GetByID)
	router.POST("/api/v1/admin/groups", groupHandler.Create)
	router.PUT("/api/v1/admin/groups/:id", groupHandler.Update)
	router.DELETE("/api/v1/admin/groups/:id", groupHandler.Delete)
	router.GET("/api/v1/admin/groups/:id/stats", groupHandler.GetStats)
	router.GET("/api/v1/admin/groups/:id/api-keys", groupHandler.GetGroupAPIKeys)

	router.GET("/api/v1/admin/proxies", proxyHandler.List)
	router.GET("/api/v1/admin/proxies/all", proxyHandler.GetAll)
	router.GET("/api/v1/admin/proxies/:id", proxyHandler.GetByID)
	router.POST("/api/v1/admin/proxies", proxyHandler.Create)
	router.PUT("/api/v1/admin/proxies/:id", proxyHandler.Update)
	router.DELETE("/api/v1/admin/proxies/:id", proxyHandler.Delete)
	router.POST("/api/v1/admin/proxies/batch-delete", proxyHandler.BatchDelete)
	router.POST("/api/v1/admin/proxies/:id/test", proxyHandler.Test)
	router.POST("/api/v1/admin/proxies/:id/quality-check", proxyHandler.CheckQuality)
	router.GET("/api/v1/admin/proxies/:id/stats", proxyHandler.GetStats)
	router.GET("/api/v1/admin/proxies/:id/accounts", proxyHandler.GetProxyAccounts)

	router.GET("/api/v1/admin/redeem-codes", redeemHandler.List)
	router.GET("/api/v1/admin/redeem-codes/:id", redeemHandler.GetByID)
	router.POST("/api/v1/admin/redeem-codes", redeemHandler.Generate)
	router.DELETE("/api/v1/admin/redeem-codes/:id", redeemHandler.Delete)
	router.POST("/api/v1/admin/redeem-codes/batch-delete", redeemHandler.BatchDelete)
	router.POST("/api/v1/admin/redeem-codes/:id/expire", redeemHandler.Expire)
	router.GET("/api/v1/admin/redeem-codes/:id/stats", redeemHandler.GetStats)

	return router, adminSvc
}

func TestUserHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?page=1&page_size=20", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	bindBody := map[string]any{
		"provider_type":    "wechat",
		"provider_key":     "wechat-main",
		"provider_subject": "union-123",
		"metadata":         map[string]any{"source": "admin-repair"},
		"channel": map[string]any{
			"channel":         "open",
			"channel_app_id":  "wx-open",
			"channel_subject": "openid-123",
		},
	}
	body, _ := json.Marshal(bindBody)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/1/auth-identities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	createBody := map[string]any{"email": "new@example.com", "password": "pass123", "balance": 1, "concurrency": 2}
	body, _ = json.Marshal(createBody)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	updateBody := map[string]any{"email": "updated@example.com"}
	body, _ = json.Marshal(updateBody)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/1", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/1/balance", bytes.NewBufferString(`{"balance":1,"operation":"add"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/api-keys", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/usage?period=today", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGroupHandlerDelegatedAdminCannotReadPrivilegedOwnerAPIKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	adminSvc.users[0].Role = service.RoleAdmin
	handler := NewGroupHandler(adminSvc, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionGroupsRead})
		c.Next()
	})
	router.GET("/api/v1/admin/groups/:id/api-keys", handler.GetGroupAPIKeys)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2/api-keys", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestGroupHandlerDelegatedAdminReceivesRedactedAPIKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := NewGroupHandler(adminSvc, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionGroupsRead})
		c.Next()
	})
	router.GET("/api/v1/admin/groups/:id/api-keys", handler.GetGroupAPIKeys)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2/api-keys", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "sk-test")
	require.Contains(t, rec.Body.String(), redactedGroupAPIKey)
}

func TestGroupHandlerDelegatedAdminCannotChangePrivilegedUserOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rate := 1.5
	rpm := 20

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodDelete, path: "/api/v1/admin/groups/2/rate-multipliers"},
		{method: http.MethodPut, path: "/api/v1/admin/groups/2/rate-multipliers", body: `{"entries":[{"user_id":2,"rate_multiplier":1.1}]}`},
		{method: http.MethodDelete, path: "/api/v1/admin/groups/2/rpm-overrides"},
		{method: http.MethodPut, path: "/api/v1/admin/groups/2/rpm-overrides", body: `{"entries":[{"user_id":2,"rpm_override":10}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			adminSvc := newStubAdminService()
			adminSvc.users[0].Role = service.RoleAdmin
			adminSvc.groupRateEntries = []service.UserGroupRateEntry{{
				UserID:         1,
				RateMultiplier: &rate,
				RPMOverride:    &rpm,
			}}
			handler := NewGroupHandler(adminSvc, nil, nil)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionGroupsWrite})
				c.Next()
			})
			router.DELETE("/api/v1/admin/groups/:id/rate-multipliers", handler.ClearGroupRateMultipliers)
			router.PUT("/api/v1/admin/groups/:id/rate-multipliers", handler.BatchSetGroupRateMultipliers)
			router.DELETE("/api/v1/admin/groups/:id/rpm-overrides", handler.ClearGroupRPMOverrides)
			router.PUT("/api/v1/admin/groups/:id/rpm-overrides", handler.BatchSetGroupRPMOverrides)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

func TestUserHandlerCreateAdminPermissionsRequiresSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	settingService := service.NewSettingService(newTestSettingRepo(), &config.Config{})
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, settingService)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
		c.Next()
	})
	router.POST("/api/v1/admin/users", handler.Create)

	body, err := json.Marshal(map[string]any{
		"email":             "delegated@example.com",
		"password":          "pass123",
		"admin_permissions": []string{service.AdminPermissionUsersRead},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Nil(t, adminSvc.createdUser)
}

func TestUserHandlerCreateAdminRoleRequiresSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
		c.Next()
	})
	router.POST("/api/v1/admin/users", handler.Create)

	body, err := json.Marshal(map[string]any{
		"email":    "promoted@example.com",
		"password": "pass123",
		"role":     service.RoleAdmin,
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Nil(t, adminSvc.createdUser)
}

func TestUserHandlerUpdateRoleRequiresSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
		c.Next()
	})
	router.PUT("/api/v1/admin/users/:id", handler.Update)

	body, err := json.Marshal(map[string]any{"role": service.RoleAdmin})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Nil(t, adminSvc.updatedUser)
}

func TestUserHandlerDelegatedAdminCannotManagePrivilegedUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		method     string
		path       string
		body       map[string]any
		assertNone func(*testing.T, *stubAdminService)
	}{
		{
			name:   "update credentials",
			method: http.MethodPut,
			path:   "/api/v1/admin/users/1",
			body:   map[string]any{"password": "changed-pass"},
			assertNone: func(t *testing.T, svc *stubAdminService) {
				require.Nil(t, svc.updatedUser)
			},
		},
		{
			name:   "bind auth identity",
			method: http.MethodPost,
			path:   "/api/v1/admin/users/1/auth-identities",
			body: map[string]any{
				"provider_type":    "oidc",
				"provider_key":     "main",
				"provider_subject": "attacker",
			},
			assertNone: func(t *testing.T, svc *stubAdminService) {
				require.Nil(t, svc.boundAuthIdentity)
			},
		},
		{
			name:   "delete delegated admin",
			method: http.MethodDelete,
			path:   "/api/v1/admin/users/1",
			assertNone: func(t *testing.T, svc *stubAdminService) {
				require.Empty(t, svc.deletedUserIDs)
			},
		},
		{
			name:   "update balance",
			method: http.MethodPost,
			path:   "/api/v1/admin/users/1/balance",
			body:   map[string]any{"balance": 1, "operation": "add"},
		},
		{
			name:   "read raw api keys",
			method: http.MethodGet,
			path:   "/api/v1/admin/users/1/api-keys",
		},
		{
			name:   "replace api key group",
			method: http.MethodPost,
			path:   "/api/v1/admin/users/1/replace-group",
			body:   map[string]any{"old_group_id": 1, "new_group_id": 2},
		},
		{
			name:   "read platform quotas",
			method: http.MethodGet,
			path:   "/api/v1/admin/users/1/platform-quotas",
		},
		{
			name:   "update platform quotas",
			method: http.MethodPut,
			path:   "/api/v1/admin/users/1/platform-quotas",
			body:   map[string]any{"quotas": []any{}},
		},
		{
			name:   "reset platform quota",
			method: http.MethodPost,
			path:   "/api/v1/admin/users/1/platform-quotas/reset",
			body:   map[string]any{"platform": "openai", "window": "daily"},
		},
		{
			name:   "read speed config",
			method: http.MethodGet,
			path:   "/api/v1/admin/users/1/speed",
		},
		{
			name:   "update speed config",
			method: http.MethodPut,
			path:   "/api/v1/admin/users/1/speed/2",
			body:   map[string]any{},
		},
		{
			name:   "reset speed usage",
			method: http.MethodPost,
			path:   "/api/v1/admin/users/1/speed/2/reset",
		},
		{
			name:   "clear speed config",
			method: http.MethodDelete,
			path:   "/api/v1/admin/users/1/speed/2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adminSvc := newStubAdminService()
			adminSvc.users[0].AdminPermissions = []string{service.AdminPermissionDashboardRead}
			handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
			handler.userPlatformQuotaRepo = &privilegedGuardQuotaRepo{}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
				c.Next()
			})
			router.PUT("/api/v1/admin/users/:id", handler.Update)
			router.POST("/api/v1/admin/users/:id/auth-identities", handler.BindAuthIdentity)
			router.DELETE("/api/v1/admin/users/:id", handler.Delete)
			router.POST("/api/v1/admin/users/:id/balance", handler.UpdateBalance)
			router.GET("/api/v1/admin/users/:id/api-keys", handler.GetUserAPIKeys)
			router.POST("/api/v1/admin/users/:id/replace-group", handler.ReplaceGroup)
			router.GET("/api/v1/admin/users/:id/platform-quotas", handler.GetUserPlatformQuotas)
			router.PUT("/api/v1/admin/users/:id/platform-quotas", handler.UpdateUserPlatformQuotas)
			router.POST("/api/v1/admin/users/:id/platform-quotas/reset", handler.ResetUserPlatformQuotaWindow)
			router.GET("/api/v1/admin/users/:id/speed", handler.GetUserSpeed)
			router.PUT("/api/v1/admin/users/:id/speed/:group_id", handler.UpdateUserSpeed)
			router.POST("/api/v1/admin/users/:id/speed/:group_id/reset", handler.ResetUserSpeed)
			router.DELETE("/api/v1/admin/users/:id/speed/:group_id", handler.ClearUserSpeedConfig)

			var body []byte
			if tt.body != nil {
				var err error
				body, err = json.Marshal(tt.body)
				require.NoError(t, err)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			if tt.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusForbidden, rec.Code)
			if tt.assertNone != nil {
				tt.assertNone(t, adminSvc)
			}
		})
	}
}

func TestUserHandlerDelegatedAdminCanManageNormalUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
		c.Next()
	})
	router.PUT("/api/v1/admin/users/:id", handler.Update)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", bytes.NewBufferString(`{"username":"managed"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, adminSvc.updatedUser)
	require.NotNil(t, adminSvc.updatedUser.Username)
	require.Equal(t, "managed", *adminSvc.updatedUser.Username)
}

func TestUserHandlerBatchUpdateConcurrencyRejectsPrivilegedUserForDelegatedAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	adminSvc.users[0].Role = service.RoleAdmin
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
		c.Next()
	})
	router.POST("/api/v1/admin/users/batch-concurrency", handler.BatchUpdateConcurrency)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/batch-concurrency", bytes.NewBufferString(`{"user_ids":[1],"concurrency":2,"mode":"set"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestUserHandlerBatchUpdateConcurrencyAllowsNormalUserForDelegatedAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminPermissions), []string{service.AdminPermissionUsersWrite})
		c.Next()
	})
	router.POST("/api/v1/admin/users/batch-concurrency", handler.BatchUpdateConcurrency)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/batch-concurrency", bytes.NewBufferString(`{"user_ids":[1],"concurrency":2,"mode":"set"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"affected":1`)
}

func TestUserHandlerCreatePassesAdminPermissionsForSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	settingService := service.NewSettingService(newTestSettingRepo(), &config.Config{})
	handler := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, settingService)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAdminSuper), true)
		c.Next()
	})
	router.POST("/api/v1/admin/users", handler.Create)

	body, err := json.Marshal(map[string]any{
		"email":             "delegated@example.com",
		"password":          "pass123",
		"role":              service.RoleAdmin,
		"admin_permissions": []string{service.AdminPermissionUsersRead},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, adminSvc.createdUser)
	require.Equal(t, service.RoleAdmin, adminSvc.createdUser.Role)
	require.NotNil(t, adminSvc.createdUser.AdminPermissions)
	require.Equal(t, []string{service.AdminPermissionUsersRead}, *adminSvc.createdUser.AdminPermissions)
}

func TestUserHandlerBindAuthIdentityMapsRequest(t *testing.T) {
	router, adminSvc := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"provider_type":    "oidc",
		"provider_key":     "https://issuer.example",
		"provider_subject": "subject-123",
		"issuer":           "https://issuer.example",
		"metadata":         map[string]any{"report_id": 12},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/9/auth-identities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(9), adminSvc.boundAuthIdentityFor)
	require.NotNil(t, adminSvc.boundAuthIdentity)
	require.Equal(t, "oidc", adminSvc.boundAuthIdentity.ProviderType)
	require.Equal(t, "https://issuer.example", adminSvc.boundAuthIdentity.ProviderKey)
	require.Equal(t, "subject-123", adminSvc.boundAuthIdentity.ProviderSubject)
	require.Nil(t, adminSvc.boundAuthIdentity.Channel)
	require.Equal(t, float64(12), adminSvc.boundAuthIdentity.Metadata["report_id"])
}

func TestGroupHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/all", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/0/models-list-candidates?platform=openai", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "gpt-5.5")

	body, _ := json.Marshal(map[string]any{"name": "new", "platform": "anthropic", "subscription_type": "standard"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ = json.Marshal(map[string]any{"name": "update"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/groups/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/groups/2", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2/api-keys", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestProxyHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/all", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/4", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ := json.Marshal(map[string]any{"name": "proxy", "protocol": "http", "host": "localhost", "port": 8080})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ = json.Marshal(map[string]any{"name": "proxy2"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/proxies/4", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/proxies/4", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/batch-delete", bytes.NewBufferString(`{"ids":[1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/4/test", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/4/quality-check", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/4/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/4/accounts", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRedeemHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/5", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ := json.Marshal(map[string]any{"count": 1, "type": "balance", "value": 10})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/redeem-codes/5", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/batch-delete", bytes.NewBufferString(`{"ids":[1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/5/expire", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/5/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
