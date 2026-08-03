package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const SettingKeyFeishuAssistantConfig = "feishu_assistant_config"

var feishuAssistantTimePattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

type FeishuAssistantConfig struct {
	Enabled            bool   `json:"enabled"`
	APIKeyID           int64  `json:"api_key_id"`
	APIKeyHint         string `json:"api_key_hint,omitempty"`
	Model              string `json:"model"`
	DailyDigestEnabled bool   `json:"daily_digest_enabled"`
	DailyDigestTime    string `json:"daily_digest_time"`
	APIKeyRequestMode  string `json:"api_key_request_mode"`
	DefaultGroupID     int64  `json:"default_group_id"`
	MaxActiveKeys      int    `json:"max_active_keys"`
}

func defaultFeishuAssistantConfig() FeishuAssistantConfig {
	return FeishuAssistantConfig{
		DailyDigestTime:   "00:05",
		APIKeyRequestMode: FeishuAPIKeyRequestModeManual,
		MaxActiveKeys:     5,
	}
}

func (s *FeishuNotificationService) GetAssistantConfig(ctx context.Context) (FeishuAssistantConfig, error) {
	cfg := defaultFeishuAssistantConfig()
	if s == nil || s.settingRepo == nil {
		return cfg, fmt.Errorf("feishu assistant settings are unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyFeishuAssistantConfig)
	if err == nil && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return cfg, fmt.Errorf("decode feishu assistant config: %w", err)
		}
	}
	normalizeFeishuAssistantConfig(&cfg)
	if cfg.APIKeyID > 0 && s.apiKeyService != nil {
		if key, keyErr := s.apiKeyService.GetByID(ctx, cfg.APIKeyID); keyErr == nil && key != nil {
			cfg.APIKeyHint = fmt.Sprintf("%s · ****%s", key.Name, opaqueLastFour(key.Key))
		}
	}
	return cfg, nil
}

func (s *FeishuNotificationService) UpdateAssistantConfig(ctx context.Context, cfg FeishuAssistantConfig) (FeishuAssistantConfig, error) {
	if s == nil || s.settingRepo == nil {
		return cfg, fmt.Errorf("feishu assistant settings are unavailable")
	}
	cfg.APIKeyHint = ""
	normalizeFeishuAssistantConfig(&cfg)
	if err := validateFeishuAssistantConfig(cfg); err != nil {
		return cfg, err
	}
	if cfg.Enabled {
		if s.apiKeyService == nil {
			return cfg, fmt.Errorf("API key service is unavailable")
		}
		key, err := s.apiKeyService.GetByID(ctx, cfg.APIKeyID)
		if err != nil || key == nil || !key.IsActive() {
			return cfg, fmt.Errorf("assistant API key is missing or inactive")
		}
		baseURL, err := s.settingRepo.GetValue(ctx, SettingKeyAPIBaseURL)
		if err != nil || strings.TrimSpace(baseURL) == "" {
			return cfg, fmt.Errorf("api_base_url must be configured before enabling the Feishu assistant")
		}
		parsed, err := url.Parse(strings.TrimSpace(baseURL))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return cfg, fmt.Errorf("api_base_url must be an absolute HTTPS URL")
		}
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyFeishuAssistantConfig, string(encoded)); err != nil {
		return cfg, err
	}
	return s.GetAssistantConfig(ctx)
}

func normalizeFeishuAssistantConfig(cfg *FeishuAssistantConfig) {
	if cfg == nil {
		return
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.DailyDigestTime = strings.TrimSpace(cfg.DailyDigestTime)
	if cfg.DailyDigestTime == "" {
		cfg.DailyDigestTime = "00:05"
	}
	cfg.APIKeyRequestMode = strings.ToLower(strings.TrimSpace(cfg.APIKeyRequestMode))
	if cfg.APIKeyRequestMode == "" {
		cfg.APIKeyRequestMode = FeishuAPIKeyRequestModeManual
	}
	if cfg.MaxActiveKeys <= 0 {
		cfg.MaxActiveKeys = 5
	}
}

func validateFeishuAssistantConfig(cfg FeishuAssistantConfig) error {
	if !feishuAssistantTimePattern.MatchString(cfg.DailyDigestTime) {
		return fmt.Errorf("daily_digest_time must use HH:MM")
	}
	if cfg.APIKeyRequestMode != FeishuAPIKeyRequestModeDisabled &&
		cfg.APIKeyRequestMode != FeishuAPIKeyRequestModeManual &&
		cfg.APIKeyRequestMode != FeishuAPIKeyRequestModeAuto {
		return fmt.Errorf("invalid API key request mode")
	}
	if cfg.MaxActiveKeys < 1 || cfg.MaxActiveKeys > 100 {
		return fmt.Errorf("max_active_keys must be between 1 and 100")
	}
	if cfg.Enabled {
		if cfg.APIKeyID <= 0 {
			return fmt.Errorf("assistant API key is required")
		}
		if cfg.Model == "" || len([]rune(cfg.Model)) > 128 {
			return fmt.Errorf("assistant model must contain 1 to 128 characters")
		}
	}
	return nil
}
