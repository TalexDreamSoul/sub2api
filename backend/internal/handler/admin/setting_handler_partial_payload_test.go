//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Saving settings is a whole-document PUT. A client that sends only the field it
// cares about must not reset everything else: a payload as small as
// `{"risk_control_enabled":true}` used to clear site_name, after which
// getStringOrDefault rendered the empty value as the built-in default and the
// login page silently changed name.

func TestUpdateSettingsPartialPayloadKeepsUnsentKeys(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName:         "Example Gateway",
		service.SettingKeySiteSubtitle:     "Example Gateway Platform",
		service.SettingKeySMTPHost:         "smtp.example.com",
		service.SettingKeySMTPFrom:         "noreply@example.com",
		service.SettingKeyTurnstileEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "true", repo.values[service.SettingKeyRiskControlEnabled],
		"the field the caller actually sent must be written")

	require.Equal(t, "Example Gateway", repo.values[service.SettingKeySiteName])
	require.Equal(t, "Example Gateway Platform", repo.values[service.SettingKeySiteSubtitle])
	require.Equal(t, "smtp.example.com", repo.values[service.SettingKeySMTPHost])
	require.Equal(t, "noreply@example.com", repo.values[service.SettingKeySMTPFrom])
	require.Equal(t, "true", repo.values[service.SettingKeyTurnstileEnabled])
}

// A full payload keeps whole-document semantics: fields explicitly set to their
// zero value are still cleared.
func TestUpdateSettingsFullPayloadStillClearsSentEmptyFields(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName: "Example Gateway",
	})

	rec := doUpdateSettings(t, h, map[string]any{"site_name": ""}, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "", repo.values[service.SettingKeySiteName],
		"an explicitly sent empty value is a deliberate clear, not an omission")
}

func prepareRuntimeSecuritySuperAdmin(c *gin.Context) {
	c.Set(string(servermiddleware.ContextKeyAdminSuper), true)
}

func TestUpdateSettingsRuntimeSecurityConfigurationIsValidatedAndRedacted(t *testing.T) {
	key := strings.Repeat("ab", 32)
	cfg := &config.Config{
		Default: config.DefaultConfig{UserConcurrency: 5},
		Totp:    config.TotpConfig{EncryptionKey: strings.Repeat("01", 32)},
	}
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, cfg)
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	totpService, _ := newRuntimeActivatableTotpService(t, cfg, svc)
	h.SetStepUpDeps(totpService, nil)

	rec := doUpdateSettings(t, h, map[string]any{
		"totp_enabled":        true,
		"totp_encryption_key": key,
		"passkey_enabled":     true,
		"passkey_rp_id":       "router.example.com",
		"passkey_rp_origins":  []string{"https://router.example.com"},
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyTotpEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyPasskeyEnabled])
	require.Equal(t, key, repo.values[service.SettingKeyTotpEncryptionKey])
	require.Equal(t, "router.example.com", repo.values[service.SettingKeyWebAuthnRPID])
	require.NotContains(t, rec.Body.String(), key, "write-only encryption key must never be returned")
}

func TestUpdateSettingsFirstTotpKeyActivatesRuntimeEncryptor(t *testing.T) {
	oldRuntimeKey := strings.Repeat("01", 32)
	newFixedKey := strings.Repeat("ab", 32)
	cfg := &config.Config{
		Default: config.DefaultConfig{UserConcurrency: 5},
		Totp:    config.TotpConfig{EncryptionKey: oldRuntimeKey},
	}
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, cfg)
	runtimeTotpService, runtimeEncryptor := newRuntimeActivatableTotpService(t, cfg, svc)
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	h.SetStepUpDeps(runtimeTotpService, nil)

	rec := doUpdateSettings(t, h, map[string]any{
		"totp_enabled":        true,
		"totp_encryption_key": newFixedKey,
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, newFixedKey, repo.values[service.SettingKeyTotpEncryptionKey])

	var response struct {
		Data struct {
			Configured      bool `json:"totp_encryption_key_configured"`
			RestartRequired bool `json:"totp_restart_required"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.True(t, response.Data.Configured)
	require.False(t, response.Data.RestartRequired)

	ciphertext, err := runtimeEncryptor.Encrypt("subsequent TOTP secret")
	require.NoError(t, err)
	plaintext, err := runtimeEncryptor.Decrypt(ciphertext)
	require.NoError(t, err)
	require.Equal(t, "subsequent TOTP secret", plaintext)

	oldEncryptor, err := repository.NewAESEncryptor(&config.Config{
		Totp: config.TotpConfig{EncryptionKey: oldRuntimeKey},
	})
	require.NoError(t, err)
	_, err = oldEncryptor.Decrypt(ciphertext)
	require.Error(t, err, "the pre-activation runtime key must not decrypt newly persisted TOTP secrets")
}

func TestUpdateSettingsPersistsWebAuthnBoundaryAtomically(t *testing.T) {
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{
		Default: config.DefaultConfig{UserConcurrency: 5},
		WebAuthn: config.WebAuthnConfig{
			Enabled:       true,
			RPDisplayName: "Sub2API",
			RPID:          "old.example.com",
			RPOrigins:     []string{"https://old.example.com"},
		},
	})
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := doUpdateSettings(t, h, map[string]any{
		"passkey_rp_origins": []string{"https://login.old.example.com"},
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "old.example.com", repo.values[service.SettingKeyWebAuthnRPID])
	require.JSONEq(t, `["https://login.old.example.com"]`, repo.values[service.SettingKeyWebAuthnRPOrigins])
}

func TestUpdateSettingsRejectsActiveWebAuthnRPIDChange(t *testing.T) {
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{
		Default: config.DefaultConfig{UserConcurrency: 5},
		WebAuthn: config.WebAuthnConfig{
			Enabled:       true,
			RPDisplayName: "Sub2API",
			RPID:          "old.example.com",
			RPOrigins:     []string{"https://old.example.com"},
		},
	})
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := doUpdateSettings(t, h, map[string]any{
		"passkey_rp_id":      "new.example.com",
		"passkey_rp_origins": []string{"https://new.example.com"},
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, repo.values)
}

func TestUpdateSettingsRejectsActiveTotpKeyRotation(t *testing.T) {
	activeKey := strings.Repeat("ab", 32)
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{
		Default: config.DefaultConfig{UserConcurrency: 5},
		Totp: config.TotpConfig{
			EncryptionKey:           activeKey,
			EncryptionKeyConfigured: true,
		},
	})
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := doUpdateSettings(t, h, map[string]any{
		"totp_encryption_key": strings.Repeat("cd", 32),
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, repo.values[service.SettingKeyTotpEncryptionKey])
}

func TestUpdateSettingsRejectsInvalidRuntimeSecurityConfiguration(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, nil)

	invalidKey := doUpdateSettings(t, h, map[string]any{
		"totp_encryption_key": "not-a-key",
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusBadRequest, invalidKey.Code)

	invalidOrigin := doUpdateSettings(t, h, map[string]any{
		"passkey_rp_id":      "router.example.com",
		"passkey_rp_origins": []string{"http://router.example.com"},
	}, prepareRuntimeSecuritySuperAdmin)
	require.Equal(t, http.StatusBadRequest, invalidOrigin.Code)
}

func TestUpdateSettingsRuntimeSecurityConfigurationRequiresSuperAdmin(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)

	rec := doUpdateSettings(t, h, map[string]any{
		"passkey_rp_id":      "router.example.com",
		"passkey_rp_origins": []string{"https://router.example.com"},
	}, nil)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, repo.values)
}

// smtp_from_email is the one request field whose JSON name differs from its
// setting key; the alias keeps it from being treated as always-omitted.
func TestUpdateSettingsSMTPFromAliasIsWritable(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySMTPFrom: "old@example.com",
	})

	rec := doUpdateSettings(t, h, map[string]any{"smtp_from_email": "new@example.com"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "new@example.com", repo.values[service.SettingKeySMTPFrom])
}
