package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

const (
	feishuAPIKeyExpiryLookahead = 7 * 24 * time.Hour
	feishuAPIKeyExpiryPoll      = 6 * time.Hour
)

type feishuExpiringAPIKeyLister interface {
	ListFeishuExpiringAPIKeys(ctx context.Context, after, through time.Time, offset, limit int) ([]APIKey, error)
}

func (s *FeishuNotificationService) runAPIKeyExpiryWorker(ctx context.Context) {
	defer s.workerWG.Done()
	s.enqueueAPIKeyExpiryReminders(ctx)
	ticker := time.NewTicker(feishuAPIKeyExpiryPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.enqueueAPIKeyExpiryReminders(ctx)
		}
	}
}

func (s *FeishuNotificationService) enqueueAPIKeyExpiryReminders(ctx context.Context) {
	lister, ok := s.apiKeyRepo.(feishuExpiringAPIKeyLister)
	if !ok || s.outboxRepo == nil {
		return
	}
	now := time.Now()
	through := now.Add(feishuAPIKeyExpiryLookahead)
	const pageSize = 1000
	for offset := 0; ; {
		keys, err := lister.ListFeishuExpiringAPIKeys(ctx, now, through, offset, pageSize)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("feishu api key expiry scan failed", "offset", offset, "error", err)
			}
			return
		}
		for i := range keys {
			if err := s.queueAPIKeyExpiryReminder(ctx, &keys[i], now); err != nil && ctx.Err() == nil {
				slog.Warn("feishu api key expiry reminder enqueue failed", "api_key_id", keys[i].ID, "user_id", keys[i].UserID, "error", err)
			}
		}
		if len(keys) < pageSize || ctx.Err() != nil {
			return
		}
		offset += len(keys)
	}
}

func (s *FeishuNotificationService) queueAPIKeyExpiryReminder(ctx context.Context, key *APIKey, now time.Time) error {
	if key == nil || key.ID <= 0 || key.UserID <= 0 || key.ExpiresAt == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	days := int(math.Ceil(key.ExpiresAt.Sub(now).Hours() / 24))
	if days < 0 {
		days = 0
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "API Key 即将到期"},
			"template": "orange",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("%s (****%s) 将在 %d 天后到期。", key.Name, opaqueLastFour(key.Key), days)}},
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "到期时间：" + key.ExpiresAt.Local().Format("2006-01-02 15:04")}},
			s.feishuPanelActionElement(ctx, "管理 API Key", ""),
		},
	}
	businessKey := fmt.Sprintf("api-key-expiry:%d:%s", key.ID, key.ExpiresAt.UTC().Format(time.RFC3339))
	return s.queueNotificationCard(ctx, key.UserID, "quota", businessKey, card)
}
