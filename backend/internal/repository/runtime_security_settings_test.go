package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestApplyPersistedRuntimeSecuritySettings(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT key, value FROM settings WHERE key = ANY($1)`)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow("totp_encryption_key", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef").
			AddRow("webauthn_rp_id", "router.example.com").
			AddRow("webauthn_rp_origins", `["https://router.example.com"]`))

	cfg := &config.Config{Totp: config.TotpConfig{
		EncryptionKey:           "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EncryptionKeyConfigured: false,
	}}
	require.NoError(t, applyPersistedRuntimeSecuritySettings(context.Background(), db, cfg))
	require.Equal(t, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", cfg.Totp.EncryptionKey)
	require.True(t, cfg.Totp.EncryptionKeyConfigured)
	require.True(t, cfg.WebAuthn.Enabled)
	require.Equal(t, "router.example.com", cfg.WebAuthn.RPID)
	require.Equal(t, []string{"https://router.example.com"}, cfg.WebAuthn.RPOrigins)
	require.NoError(t, mock.ExpectationsWereMet())
}
