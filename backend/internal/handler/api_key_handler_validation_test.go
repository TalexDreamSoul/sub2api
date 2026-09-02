//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateAPIKeyCreateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(1)
	positiveDays, zeroDays, negativeDays := 1, 0, -1
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Quota: &zero, RateLimit5h: &large, ExpiresInDays: &positiveDays}))

	for _, req := range []CreateAPIKeyRequest{
		{Quota: &negative},
		{Quota: &nan},
		{RateLimit5h: &inf},
		{RateLimit1d: &negative},
		{RateLimit7d: &negative},
		{ExpiresInDays: &zeroDays},
		{ExpiresInDays: &negativeDays},
	} {
		require.Error(t, validateAPIKeyCreateRequest(req))
	}
}

func TestValidateAPIKeyUpdateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(-1)
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Quota: &zero, RateLimit7d: &large}))

	for _, req := range []UpdateAPIKeyRequest{
		{Quota: &negative},
		{RateLimit5h: &nan},
		{RateLimit1d: &inf},
		{RateLimit7d: &negative},
	} {
		require.Error(t, validateAPIKeyUpdateRequest(req))
	}
}

type apiKeyRuntimeStatusCacheStub struct {
	service.APIKeyRuntimeCache
	activeIPs []service.APIKeyActiveIP
}

func (s *apiKeyRuntimeStatusCacheStub) GetActiveIPs(context.Context, int64, time.Duration) ([]service.APIKeyActiveIP, error) {
	return s.activeIPs, nil
}

func TestAPIKeyHandlerGetRuntimeReturnsEffectiveHiddenUserActiveIPLimit(t *testing.T) {
	const userID int64 = 42
	apiKeyRepo := &dailyUsageAPIKeyRepoStub{
		keys: map[int64]*service.APIKey{
			7: {
				ID:           7,
				UserID:       userID,
				MaxActiveIPs: 0,
				User: &service.User{
					APIKeyMaxActiveIPs:        1,
					APIKeyMaxActiveIPsVisible: false,
				},
			},
		},
	}
	runtimeCache := &apiKeyRuntimeStatusCacheStub{
		activeIPs: []service.APIKeyActiveIP{{IP: "203.0.113.7"}},
	}
	handler := ProvideAPIKeyHandler(
		service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, nil),
		service.NewAPIKeyRuntimeService(runtimeCache),
	)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		c.Next()
	})
	router.GET("/keys/:id/runtime", handler.GetRuntime)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/keys/7/runtime", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var got struct {
		Code int `json:"code"`
		Data struct {
			MaxActiveIPs int `json:"max_active_ips"`
			ActiveIPCount int `json:"active_ip_count"`
			ActiveIPs []struct {
				IP string `json:"ip"`
			} `json:"active_ips"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
	require.Equal(t, 0, got.Code)
	require.Equal(t, 1, got.Data.MaxActiveIPs)
	require.Equal(t, 1, got.Data.ActiveIPCount)
	require.Equal(t, "203.0.113.7", got.Data.ActiveIPs[0].IP)
}
