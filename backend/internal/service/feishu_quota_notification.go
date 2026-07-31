package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type FeishuAPIKeyQuotaExhaustedNotification struct {
	UserID          int64
	APIKeyID        int64
	APIKeyName      string
	APIKeyValue     string
	QuotaUSD        float64
	SourceRequestID string
}

func (s *FeishuNotificationService) QueueAPIKeyQuotaExhausted(ctx context.Context, input FeishuAPIKeyQuotaExhaustedNotification) error {
	if input.UserID <= 0 || input.APIKeyID <= 0 || strings.TrimSpace(input.SourceRequestID) == "" {
		return nil
	}
	content := fmt.Sprintf("%s (****%s) 已达到本站配置额度上限", input.APIKeyName, opaqueLastFour(input.APIKeyValue))
	if input.QuotaUSD > 0 {
		content += fmt.Sprintf(" $%.2f", input.QuotaUSD)
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "API Key 额度已用尽"},
			"template": "red",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": content}},
			s.feishuPanelActionElement(ctx, "管理 API Key", ""),
		},
	}
	businessKey := fmt.Sprintf("api-key-quota-exhausted:%d:%s", input.APIKeyID, strings.TrimSpace(input.SourceRequestID))
	return s.queueNotificationCard(ctx, input.UserID, "quota", businessKey, card)
}

func (s *BalanceNotifyService) NotifyAPIKeyQuotaExhausted(ctx context.Context, user *User, key *APIKey, sourceRequestID string) {
	if s == nil || s.feishuNotificationService == nil || user == nil || key == nil {
		return
	}
	if err := s.feishuNotificationService.QueueAPIKeyQuotaExhausted(ctx, FeishuAPIKeyQuotaExhaustedNotification{
		UserID: user.ID, APIKeyID: key.ID, APIKeyName: key.Name, APIKeyValue: key.Key,
		QuotaUSD: key.Quota, SourceRequestID: sourceRequestID,
	}); err != nil {
		// Notification failure must not roll back an already committed charge.
		slog.Warn("feishu api key quota notification enqueue failed", "user_id", user.ID, "api_key_id", key.ID, "error", err)
	}
}
