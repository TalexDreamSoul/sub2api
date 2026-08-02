package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func applyPersistedRuntimeSecuritySettings(ctx context.Context, db *sql.DB, cfg *config.Config) error {
	if db == nil || cfg == nil {
		return nil
	}
	keys := []string{
		service.SettingKeyTotpEncryptionKey,
		service.SettingKeyWebAuthnRPID,
		service.SettingKeyWebAuthnRPOrigins,
	}
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM settings WHERE key = ANY($1)`, pq.Array(keys))
	if err != nil {
		return fmt.Errorf("load persisted runtime security settings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	values := make(map[string]string, len(keys))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("scan persisted runtime security setting: %w", err)
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate persisted runtime security settings: %w", err)
	}

	if key := values[service.SettingKeyTotpEncryptionKey]; key != "" {
		if len(key) != 64 {
			return fmt.Errorf("persisted TOTP encryption key must be 64 hexadecimal characters")
		}
		for _, ch := range key {
			if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
				return fmt.Errorf("persisted TOTP encryption key must be 64 hexadecimal characters")
			}
		}
		cfg.Totp.EncryptionKey = strings.ToLower(key)
		cfg.Totp.EncryptionKeyConfigured = true
	}

	rpID := values[service.SettingKeyWebAuthnRPID]
	rawOrigins := values[service.SettingKeyWebAuthnRPOrigins]
	if rpID == "" && rawOrigins == "" {
		return nil
	}
	if rpID == "" || rawOrigins == "" {
		slog.Warn("ignoring incomplete persisted WebAuthn configuration",
			"rp_id_present", rpID != "",
			"origins_present", rawOrigins != "")
		return nil
	}
	var origins []string
	if err := json.Unmarshal([]byte(rawOrigins), &origins); err != nil {
		return fmt.Errorf("parse persisted WebAuthn RP origins: %w", err)
	}
	cfg.WebAuthn.Enabled = true
	cfg.WebAuthn.RPID = rpID
	cfg.WebAuthn.RPOrigins = origins
	if strings.TrimSpace(cfg.WebAuthn.RPDisplayName) == "" {
		cfg.WebAuthn.RPDisplayName = "Sub2API"
	}
	if err := config.NormalizeAndValidateWebAuthnConfig(&cfg.WebAuthn); err != nil {
		return fmt.Errorf("validate persisted WebAuthn configuration: %w", err)
	}
	return nil
}
