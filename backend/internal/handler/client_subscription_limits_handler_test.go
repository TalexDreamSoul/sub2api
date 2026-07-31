//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type clientSubscriptionLimitsAccountRepo struct {
	service.AccountRepository
	accounts    map[int64]*service.Account
	schedulable []service.Account
}

func (r *clientSubscriptionLimitsAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	account := r.accounts[id]
	if account == nil {
		return nil, errors.New("account not found")
	}
	return account, nil
}

func (r *clientSubscriptionLimitsAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, _ string) ([]service.Account, error) {
	return r.schedulable, nil
}

type clientSubscriptionLimitsTokenCache struct {
	tokens map[string]string
}

func (c *clientSubscriptionLimitsTokenCache) GetAccessToken(_ context.Context, key string) (string, error) {
	token, ok := c.tokens[key]
	if !ok {
		return "", errors.New("token not found")
	}
	return token, nil
}

func (*clientSubscriptionLimitsTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	return nil
}

func (*clientSubscriptionLimitsTokenCache) DeleteAccessToken(context.Context, string) error {
	return nil
}

func (*clientSubscriptionLimitsTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (*clientSubscriptionLimitsTokenCache) ReleaseRefreshLock(context.Context, string) error {
	return nil
}

func newClientSubscriptionLimitsHandler(t *testing.T) *ClientSubscriptionLimitsHandler {
	t.Helper()

	firstAccount := &service.Account{
		ID:       101,
		Name:     "account-one-private-name",
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": "private-account-one",
		},
	}
	secondAccount := &service.Account{
		ID:       102,
		Name:     "account-two-private-name",
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": "private-account-two",
		},
	}
	ignoredAPIKeyAccount := service.Account{
		ID:       103,
		Name:     "api-key-account-private-name",
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Status:   service.StatusActive,
		Credentials: map[string]any{
			"api_key": "private-upstream-api-key",
		},
	}
	repo := &clientSubscriptionLimitsAccountRepo{
		accounts: map[int64]*service.Account{
			firstAccount.ID:  firstAccount,
			secondAccount.ID: secondAccount,
		},
		schedulable: []service.Account{*firstAccount, *secondAccount, ignoredAPIKeyAccount},
	}
	tokenCache := &clientSubscriptionLimitsTokenCache{tokens: map[string]string{
		service.OpenAITokenCacheKey(firstAccount):  "private-oauth-token-one",
		service.OpenAITokenCacheKey(secondAccount): "private-oauth-token-two",
	}}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			switch r.Header.Get("chatgpt-account-id") {
			case "private-account-one":
				_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":18000,"reset_at":1767225900},"secondary_window":{"used_percent":40,"limit_window_seconds":604800,"reset_at":1767830400}}}`))
			case "private-account-two":
				_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":60,"limit_window_seconds":18000,"reset_at":1767225600},"secondary_window":{"used_percent":80,"limit_window_seconds":604800,"reset_at":1767744000}}}`))
			default:
				http.Error(w, "unexpected account", http.StatusBadRequest)
			}
		case "/backend-api/wham/rate-limit-reset-credits":
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	quotaService := service.NewOpenAIQuotaService(
		repo,
		nil,
		service.NewOpenAITokenProvider(repo, tokenCache, nil),
		func(string) (*req.Client, error) {
			return req.C().WrapRoundTripFunc(func(rt req.RoundTripper) req.RoundTripFunc {
				return func(r *req.Request) (*req.Response, error) {
					r.URL.Scheme = target.Scheme
					r.URL.Host = target.Host
					return rt.RoundTrip(r)
				}
			}), nil
		},
	)
	return NewClientSubscriptionLimitsHandler(service.NewClientSubscriptionLimitsService(repo, quotaService))
}

func serveClientSubscriptionLimits(t *testing.T, handler *ClientSubscriptionLimitsHandler, apiKey *service.APIKey) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
		c.Next()
	})
	router.GET("/api/v1/client/subscription-limits", handler.Get)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/client/subscription-limits", nil))
	return recorder
}

func TestClientSubscriptionLimitsHandlerReturnsSanitizedAccountPoolAggregate(t *testing.T) {
	handler := newClientSubscriptionLimitsHandler(t)
	before := time.Now().UTC()
	recorder := serveClientSubscriptionLimits(t, handler, &service.APIKey{
		Group: &service.Group{ID: 9, Platform: service.PlatformOpenAI},
	})
	after := time.Now().UTC()

	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Provider string                                   `json:"provider"`
		Scope    string                                   `json:"scope"`
		AsOf     time.Time                                `json:"as_of"`
		Windows  []service.ClientSubscriptionLimitWindow `json:"windows"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "openai-codex", response.Provider)
	require.Equal(t, "account-pool", response.Scope)
	require.True(t, !response.AsOf.Before(before) && !response.AsOf.After(after), "as_of must describe this aggregate")
	require.Equal(t, time.UTC, response.AsOf.Location())
	require.Equal(t, []service.ClientSubscriptionLimitWindow{
		{ID: "1w", UsedPercent: 60, ResetAt: "2026-01-07T00:00:00Z"},
		{ID: "5h", UsedPercent: 40, ResetAt: "2026-01-01T00:00:00Z"},
	}, response.Windows)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &fields))
	require.Len(t, fields, 4)
	require.Contains(t, fields, "as_of")
	require.Contains(t, fields, "provider")
	require.Contains(t, fields, "scope")
	require.Contains(t, fields, "windows")
	require.NotContains(t, recorder.Body.String(), "private-account")
	require.NotContains(t, recorder.Body.String(), "private-oauth-token")
	require.NotContains(t, recorder.Body.String(), "private-upstream-api-key")
	require.NotContains(t, recorder.Body.String(), "private-name")
	require.NotContains(t, recorder.Body.String(), `"account_id"`)
	require.NotContains(t, recorder.Body.String(), `"credentials"`)
}

func TestClientSubscriptionLimitsHandlerRejectsNonOpenAIGroup(t *testing.T) {
	handler := NewClientSubscriptionLimitsHandler(service.NewClientSubscriptionLimitsService(nil, nil))
	recorder := serveClientSubscriptionLimits(t, handler, &service.APIKey{
		Group: &service.Group{ID: 9, Platform: service.PlatformAnthropic},
	})

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.JSONEq(t, `{"error":"Subscription limits are unavailable for this API key"}`, recorder.Body.String())
}

