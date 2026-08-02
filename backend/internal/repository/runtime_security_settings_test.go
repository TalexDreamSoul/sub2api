package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestApplyPersistedRuntimeSecuritySettingsTotpAndWebAuthn(t *testing.T) {
	persistedTotpKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	initialWebAuthn := config.WebAuthnConfig{
		Enabled:       true,
		RPDisplayName: "Existing RP",
		RPID:          "existing.example.com",
		RPOrigins:     []string{"https://existing.example.com"},
	}

	tests := []struct {
		name          string
		settings      *sqlmock.Rows
		wantWebAuthn  config.WebAuthnConfig
		wantErrorPart string
	}{
		{
			name: "loads complete WebAuthn configuration",
			settings: sqlmock.NewRows([]string{"key", "value"}).
				AddRow("totp_encryption_key", persistedTotpKey).
				AddRow("webauthn_rp_id", "router.example.com").
				AddRow("webauthn_rp_origins", `["https://router.example.com"]`),
			wantWebAuthn: config.WebAuthnConfig{
				Enabled:       true,
				RPDisplayName: "Existing RP",
				RPID:          "router.example.com",
				RPOrigins:     []string{"https://router.example.com"},
			},
		},
		{
			name: "ignores incomplete WebAuthn configuration with only RP ID",
			settings: sqlmock.NewRows([]string{"key", "value"}).
				AddRow("totp_encryption_key", persistedTotpKey).
				AddRow("webauthn_rp_id", "router.example.com"),
			wantWebAuthn: initialWebAuthn,
		},
		{
			name: "ignores incomplete WebAuthn configuration with only origins",
			settings: sqlmock.NewRows([]string{"key", "value"}).
				AddRow("totp_encryption_key", persistedTotpKey).
				AddRow("webauthn_rp_origins", `["https://router.example.com"]`),
			wantWebAuthn: initialWebAuthn,
		},
		{
			name: "rejects invalid origins in complete WebAuthn configuration",
			settings: sqlmock.NewRows([]string{"key", "value"}).
				AddRow("totp_encryption_key", persistedTotpKey).
				AddRow("webauthn_rp_id", "router.example.com").
				AddRow("webauthn_rp_origins", "not-json"),
			wantWebAuthn:  initialWebAuthn,
			wantErrorPart: "parse persisted WebAuthn RP origins",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			mock.ExpectQuery(regexp.QuoteMeta(`SELECT key, value FROM settings WHERE key = ANY($1)`)).
				WithArgs(sqlmock.AnyArg()).
				WillReturnRows(tt.settings)

			cfg := &config.Config{
				Totp: config.TotpConfig{
					EncryptionKey:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					EncryptionKeyConfigured: false,
				},
				WebAuthn: initialWebAuthn,
			}
			err = applyPersistedRuntimeSecuritySettings(context.Background(), db, cfg)
			if tt.wantErrorPart == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErrorPart)
			}

			require.Equal(t, persistedTotpKey, cfg.Totp.EncryptionKey)
			require.True(t, cfg.Totp.EncryptionKeyConfigured)
			require.Equal(t, tt.wantWebAuthn, cfg.WebAuthn)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
