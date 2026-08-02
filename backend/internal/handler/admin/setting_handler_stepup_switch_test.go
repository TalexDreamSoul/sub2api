package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// step-up 开关转换的门控测试。
// 启用时先验证系统 TOTP 已在运行时可用，再验证当前 JWT 管理员的个人 TOTP；
// 测试覆盖两层前置条件及其拒绝时不得持久化开关的契约。

func newStepUpSwitchTestHandler(t *testing.T, stored map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: stored}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

// newStepUpSwitchTestHandlerWithSystemTotpRuntime 构造已加载固定密钥且全局 TOTP 已开启的运行时，
// 使测试能穿过系统前置条件，覆盖后续的 JWT 和机器凭证门控。
func newStepUpSwitchTestHandlerWithSystemTotpRuntime(t *testing.T, stored map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	t.Helper()
	if stored == nil {
		stored = map[string]string{}
	}
	stored[service.SettingKeyTotpEnabled] = "true"
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: stored}
	svc := service.NewSettingService(repo, &config.Config{
		Totp: config.TotpConfig{
			EncryptionKey:           strings.Repeat("ab", 32),
			EncryptionKeyConfigured: true,
		},
		Default: config.DefaultConfig{UserConcurrency: 5},
	})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

type stepUpSwitchUserRepoStub struct {
	service.UserRepository
	user *service.User
}

func (s *stepUpSwitchUserRepoStub) GetByID(context.Context, int64) (*service.User, error) {
	return s.user, nil
}

func (*stepUpSwitchUserRepoStub) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func doUpdateSettings(t *testing.T, h *SettingHandler, body map[string]any, prepare func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	if prepare != nil {
		prepare(c)
	}

	h.UpdateSettings(c)
	return rec
}

func newRuntimeActivatableTotpService(t *testing.T, cfg *config.Config, settingService *service.SettingService) (*service.TotpService, service.SecretEncryptor) {
	t.Helper()
	encryptor, err := repository.NewAESEncryptor(cfg)
	require.NoError(t, err)
	return service.NewTotpService(nil, encryptor, nil, settingService, nil, nil), encryptor
}

// 首次配置时系统 TOTP 尚未在运行时可用：先返回明确前置条件错误，且不保存开关。
func TestUpdateSettingsEnableStepUpRequiresSystemTotpRuntime(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ENABLE_REQUIRES_SYSTEM_TOTP")
	_, persisted := repo.values[service.SettingKeyStepUpEnabled]
	require.False(t, persisted)
}

// 系统 TOTP 已在运行时可用后：admin API key（机器凭证）仍一律拒绝。
func TestUpdateSettingsEnableStepUpRejectsAdminAPIKey(t *testing.T) {
	h, _ := newStepUpSwitchTestHandlerWithSystemTotpRuntime(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
}

// 系统 TOTP 已在运行时可用后：有认证 JWT 会话但 userService 未注入时 fail-closed（500），不得放行。
func TestUpdateSettingsEnableStepUpFailsClosedWithoutUserService(t *testing.T) {
	h, repo := newStepUpSwitchTestHandlerWithSystemTotpRuntime(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotEqual(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 系统 TOTP 未在当前进程可用时，关闭误开的 step-up 必须允许恢复且持久化为 false。
func TestUpdateSettingsStepUpDisableWithoutSystemTotpAllowsRecovery(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
}

// 系统 TOTP 运行时，未启用个人 TOTP 的操作者仍不得关闭 step-up，且不得持久化变更。
func TestUpdateSettingsStepUpDisableWithSystemTotpRequiresPersonalTotp(t *testing.T) {
	h, repo := newStepUpSwitchTestHandlerWithSystemTotpRuntime(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})
	h.SetStepUpDeps(nil, service.NewUserService(&stepUpSwitchUserRepoStub{
		user: &service.User{ID: 1, TotpEnabled: false},
	}, nil, nil, nil))

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_TOTP_NOT_ENABLED")
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭开关：管理员系统密钥作为部署级凭证可直接执行。
func TestUpdateSettingsDisableStepUpAllowsAdminAPIKey(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
}

// 无状态转换（false→false）：不触发任何转换校验，常规保存成功且默认持久化为 false。
func TestUpdateSettingsStepUpNoTransitionSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	// 会话 IP/UA 绑定默认关闭：未显式提交时持久化 false。
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

// 保持开启（true→true）：不触发转换校验，常规保存不被打断。
func TestUpdateSettingsStepUpKeepEnabledSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 省略字段=保持现值：不含 step_up_enabled/session_binding_enabled 的旧客户端全量保存
// 不得把已开启的安全开关静默重置，也不触发任何转换门控。
func TestUpdateSettingsOmittedSecuritySwitchesKeepStoredValues(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled:         "true",
		service.SettingKeySessionBindingEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeySessionBindingEnabled])
}

// 省略字段在开关本就关闭时同样保持关闭（默认值路径）。
func TestUpdateSettingsOmittedSecuritySwitchesKeepDisabled(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

func TestUpdateSettingsForwardedClientIPHeadersOmittedPreservesAndEmptyClears(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyForwardedClientIPHeaders: `["X-Cdn-Ip","True-Client-Ip"]`,
	})

	preserved := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)
	require.Equal(t, http.StatusOK, preserved.Code)
	require.JSONEq(t, `["X-Cdn-Ip","True-Client-Ip"]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	require.Contains(t, preserved.Body.String(), `"forwarded_client_ip_headers":["X-Cdn-Ip","True-Client-Ip"]`)

	cleared := doUpdateSettings(t, h, map[string]any{"forwarded_client_ip_headers": []string{}}, nil)
	require.Equal(t, http.StatusOK, cleared.Code)
	require.JSONEq(t, `[]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	require.Contains(t, cleared.Body.String(), `"forwarded_client_ip_headers":[]`)
}

func TestUpdateSettingsMalformedForwardedClientIPHeadersRemainFailClosedWhenOmitted(t *testing.T) {
	cfg := &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}}
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyAPIKeyACLTrustForwardedIP: "true",
		service.SettingKeyForwardedClientIPHeaders:  `{"not":"an array"}`,
	}}
	svc := service.NewSettingService(repo, cfg)
	require.ErrorContains(t, svc.LoadForwardedClientIPSettings(context.Background()), "load forwarded client ip headers")
	require.False(t, cfg.ForwardedClientIPSettings().TrustForwardedIP)
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyAPIKeyACLTrustForwardedIP])
	require.JSONEq(t, `[]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	runtimeSettings := cfg.ForwardedClientIPSettings()
	require.False(t, runtimeSettings.TrustForwardedIP)
	require.Empty(t, runtimeSettings.Headers)
	require.Contains(t, rec.Body.String(), `"api_key_acl_trust_forwarded_ip":false`)
	require.Contains(t, rec.Body.String(), `"forwarded_client_ip_headers":[]`)
}

func TestUpdateSettingsRejectsInvalidForwardedClientIPHeader(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyForwardedClientIPHeaders: `["X-Existing-IP"]`,
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"forwarded_client_ip_headers": []string{"X Invalid"},
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `["X-Existing-IP"]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
}
