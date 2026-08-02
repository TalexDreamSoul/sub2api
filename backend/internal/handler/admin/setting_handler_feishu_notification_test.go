package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type settingHandlerFeishuBindingRepo struct {
	binding *service.FeishuUserIdentityBinding
}

func (r *settingHandlerFeishuBindingRepo) UpsertFeishuUserIdentityBinding(ctx context.Context, input service.UpsertFeishuUserIdentityBindingInput) (*service.FeishuUserIdentityBinding, error) {
	return nil, service.ErrFeishuNotificationConflict
}

func (r *settingHandlerFeishuBindingRepo) GetFeishuNotificationBinding(ctx context.Context, userID int64, appID string) (*service.FeishuUserIdentityBinding, error) {
	if r == nil || r.binding == nil || r.binding.UserID != userID || r.binding.AppID != appID {
		return nil, service.ErrFeishuNotificationNotBound
	}
	return r.binding, nil
}

func (r *settingHandlerFeishuBindingRepo) GetFeishuBindingByUnionID(ctx context.Context, appID, tenantKey, unionID, purpose string) (*service.FeishuUserIdentityBinding, error) {
	return nil, service.ErrFeishuNotificationNotBound
}

func (r *settingHandlerFeishuBindingRepo) ListFeishuBindingsByUser(ctx context.Context, userID int64) ([]service.FeishuUserIdentityBinding, error) {
	return nil, nil
}

func (r *settingHandlerFeishuBindingRepo) SetFeishuNotificationEnabled(ctx context.Context, userID int64, appID string, enabled bool) (*service.FeishuUserIdentityBinding, error) {
	return nil, service.ErrFeishuNotificationNotBound
}

func (r *settingHandlerFeishuBindingRepo) DeleteFeishuNotificationBinding(ctx context.Context, userID int64, appID string) error {
	return nil
}

func TestFeishuNotificationSettingSecurityHelpers(t *testing.T) {
	require.True(t, hasFeishuNotificationSettingFields(map[string]json.RawMessage{"feishu_notify_app_secret": nil}))
	require.False(t, hasFeishuNotificationSettingFields(map[string]json.RawMessage{"site_name": nil}))
	require.NoError(t, validateFeishuOfficialAPIURL("https://open.feishu.cn/open-apis/im/v1/messages"))
	require.NoError(t, validateFeishuOfficialAPIURL("https://open.larksuite.com/open-apis/im/v1/messages"))
	require.Error(t, validateFeishuOfficialAPIURL("http://open.feishu.cn/open-apis/im/v1/messages"))
	require.Error(t, validateFeishuOfficialAPIURL("https://example.com/token"))
	require.NoError(t, validateFeishuPanelURL("/feishu/panel"))
	require.NoError(t, validateFeishuPanelURL("https://app.example.com/feishu/panel"))
	require.Error(t, validateFeishuPanelURL("http://app.example.com/feishu/panel"))
}

func TestSettingHandler_UpdateSettings_PreservesFeishuNotifySecretWhenOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyFeishuNotifyEnabled:           "true",
		service.SettingKeyFeishuNotifyAppID:             "cli-old",
		service.SettingKeyFeishuNotifyAppSecret:         "old-secret",
		service.SettingKeyFeishuNotifyVerificationToken: "old-token",
		service.SettingKeyFeishuNotifyEncryptKey:        "old-encrypt-key",
	}}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rawBody, err := json.Marshal(map[string]any{
		"site_name": "Updated Site",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "old-secret", repo.values[service.SettingKeyFeishuNotifyAppSecret])
	require.Equal(t, "old-token", repo.values[service.SettingKeyFeishuNotifyVerificationToken])
	require.Equal(t, "old-encrypt-key", repo.values[service.SettingKeyFeishuNotifyEncryptKey])

	var resp response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["feishu_notify_app_secret_configured"])
	require.Equal(t, true, data["feishu_notify_verification_token_configured"])
	require.Equal(t, true, data["feishu_notify_encrypt_key_configured"])
}

func TestSettingHandler_TestFeishuNotificationRequiresStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var messageCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
			}))
		case "/messages":
			messageCalled.Store(true)
			require.Equal(t, "open_id", r.URL.Query().Get("receive_id_type"))
			require.Equal(t, "Bearer tenant-token", r.Header.Get("Authorization"))

			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "ou-test", body["receive_id"])
			require.Equal(t, "interactive", body["msg_type"])
			require.Contains(t, body["content"], "飞书通知链路测试")
			require.Contains(t, body["content"], "/feishu/panel")

			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0}))
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	settingRepo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyFeishuNotifyEnabled:    "true",
		service.SettingKeyFeishuNotifyAppID:      "cli-test",
		service.SettingKeyFeishuNotifyAppSecret:  "secret",
		service.SettingKeyFeishuNotifyTokenURL:   server.URL + "/token",
		service.SettingKeyFeishuNotifyMessageURL: server.URL + "/messages",
		service.SettingKeyFeishuNotifyPanelURL:   "/feishu/panel",
	}}
	bindingRepo := &settingHandlerFeishuBindingRepo{binding: &service.FeishuUserIdentityBinding{
		UserID:              42,
		AppID:               "cli-test",
		OpenID:              "ou-test",
		NotificationEnabled: true,
	}}
	handler := NewSettingHandler(nil, nil, nil, nil, nil, nil, nil)
	handler.SetFeishuNotificationService(service.NewFeishuNotificationService(settingRepo, bindingRepo))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/feishu-notification/test", strings.NewReader(`{"user_id":42}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.TestFeishuNotification(c)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, messageCalled.Load())
}

func TestSettingHandler_UpdateSettings_TotpWithUnchangedFeishuPayloadSkipsStepUp(t *testing.T) {
	const (
		appID      = "cli-stable"
		tokenURL   = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
		messageURL = "https://open.feishu.cn/open-apis/im/v1/messages"
		panelURL   = "/feishu/panel"
	)
	totpEncryptionKey := strings.Repeat("ab", 32)

	for _, tc := range []struct {
		name          string
		includeSecret bool
	}{
		{name: "empty secret", includeSecret: true},
		{name: "omitted secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{
				service.SettingKeyTotpEnabled:            "false",
				service.SettingKeyFeishuNotifyEnabled:    "true",
				service.SettingKeyFeishuNotifyAppID:      appID,
				service.SettingKeyFeishuNotifyAppSecret:  "stored-secret",
				service.SettingKeyFeishuNotifyTokenURL:   tokenURL,
				service.SettingKeyFeishuNotifyMessageURL: messageURL,
				service.SettingKeyFeishuNotifyPanelURL:   panelURL,
			}}
			svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
			handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

			body := map[string]any{
				"totp_enabled":              true,
				"totp_encryption_key":       totpEncryptionKey,
				"feishu_notify_enabled":     true,
				"feishu_notify_app_id":      appID,
				"feishu_notify_token_url":   tokenURL,
				"feishu_notify_message_url": messageURL,
				"feishu_notify_panel_url":   panelURL,
			}
			if tc.includeSecret {
				body["feishu_notify_app_secret"] = ""
			}

			rec := doUpdateSettings(t, handler, body, func(c *gin.Context) {
				c.Set(string(servermiddleware.ContextKeyAdminSuper), true)
			})

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "true", repo.values[service.SettingKeyTotpEnabled])
			require.Equal(t, totpEncryptionKey, repo.values[service.SettingKeyTotpEncryptionKey])
		})
	}
}

func TestSettingHandler_UpdateSettings_TotpWithChangedFeishuPayloadRequiresStepUp(t *testing.T) {
	const (
		appID      = "cli-stable"
		tokenURL   = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
		messageURL = "https://open.feishu.cn/open-apis/im/v1/messages"
		panelURL   = "/feishu/panel"
	)
	totpEncryptionKey := strings.Repeat("cd", 32)

	for _, tc := range []struct {
		name  string
		field string
		value any
	}{
		{name: "enabled", field: "feishu_notify_enabled", value: false},
		{name: "app ID", field: "feishu_notify_app_id", value: "cli-changed"},
		{name: "token URL", field: "feishu_notify_token_url", value: "https://open.feishu.cn/open-apis/auth/v3/app_access_token/internal"},
		{name: "message URL", field: "feishu_notify_message_url", value: "https://open.feishu.cn/open-apis/im/v1/messages/batch"},
		{name: "panel URL", field: "feishu_notify_panel_url", value: "/feishu/updated-panel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{
				service.SettingKeyTotpEnabled:            "false",
				service.SettingKeyFeishuNotifyEnabled:    "true",
				service.SettingKeyFeishuNotifyAppID:      appID,
				service.SettingKeyFeishuNotifyAppSecret:  "stored-secret",
				service.SettingKeyFeishuNotifyTokenURL:   tokenURL,
				service.SettingKeyFeishuNotifyMessageURL: messageURL,
				service.SettingKeyFeishuNotifyPanelURL:   panelURL,
			}}
			svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
			handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
			body := map[string]any{
				"totp_enabled":              true,
				"totp_encryption_key":       totpEncryptionKey,
				"feishu_notify_enabled":     true,
				"feishu_notify_app_id":      appID,
				"feishu_notify_token_url":   tokenURL,
				"feishu_notify_message_url": messageURL,
				"feishu_notify_panel_url":   panelURL,
			}
			body[tc.field] = tc.value

			rec := doUpdateSettings(t, handler, body, func(c *gin.Context) {
				c.Set(string(servermiddleware.ContextKeyAdminSuper), true)
			})

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Equal(t, "false", repo.values[service.SettingKeyTotpEnabled])
			_, saved := repo.values[service.SettingKeyTotpEncryptionKey]
			require.False(t, saved)
			require.Equal(t, "true", repo.values[service.SettingKeyFeishuNotifyEnabled])
			require.Equal(t, appID, repo.values[service.SettingKeyFeishuNotifyAppID])
			require.Equal(t, tokenURL, repo.values[service.SettingKeyFeishuNotifyTokenURL])
			require.Equal(t, messageURL, repo.values[service.SettingKeyFeishuNotifyMessageURL])
			require.Equal(t, panelURL, repo.values[service.SettingKeyFeishuNotifyPanelURL])
		})
	}
}
