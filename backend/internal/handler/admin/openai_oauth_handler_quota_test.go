//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAIQuotaHandlerAccountRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r *openAIQuotaHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if r.account != nil && r.account.ID == id {
		return r.account, nil
	}
	return nil, service.ErrAccountNotFound
}

func TestOpenAIOAuthHandlerQueryQuotaReturnsAccountRecordID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamReq *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"user_id":"upstream-user",
			"account_id":"upstream-account",
			"email":"owner@example.com",
			"plan_type":"plus",
			"rate_limit":{
				"allowed":true,
				"limit_reached":false,
				"primary_window":{
					"used_percent":0.25,
					"limit_window_seconds":18000,
					"reset_after_seconds":3600,
					"reset_at":1893456000
				}
			}
		}`))
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	repo := &openAIQuotaHandlerAccountRepo{account: &service.Account{
		ID:       42,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "access-token",
			"chatgpt_account_id": "chatgpt-account",
			"expires_at":         time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}}
	tokenProvider := service.NewOpenAITokenProvider(repo, nil, nil)
	quotaService := service.NewOpenAIQuotaService(repo, nil, tokenProvider, func(_ string) (*req.Client, error) {
		client := req.C().WrapRoundTripFunc(func(rt req.RoundTripper) req.RoundTripFunc {
			return func(r *req.Request) (*req.Response, error) {
				r.URL.Scheme = targetURL.Scheme
				r.URL.Host = targetURL.Host
				return rt.RoundTrip(r)
			}
		})
		return client, nil
	})
	handler := NewOpenAIOAuthHandler(nil, nil, quotaService, nil)

	router := gin.New()
	router.GET("/api/v1/admin/accounts/:id/quota", handler.QueryQuota)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/quota", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"account_record_id":42`)
	require.Contains(t, rec.Body.String(), `"account_id":"upstream-account"`)
	require.Contains(t, rec.Body.String(), `"plan_type":"plus"`)
	require.NotContains(t, rec.Body.String(), "access-token")
	require.NotNil(t, upstreamReq)
	require.Equal(t, "Bearer access-token", upstreamReq.Header.Get("Authorization"))
	require.Equal(t, "chatgpt-account", upstreamReq.Header.Get("chatgpt-account-id"))
}
