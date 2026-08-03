package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	feishuOutboxPollInterval  = 2 * time.Second
	feishuOutboxLease         = 4 * time.Minute
	feishuOutboxBatchSize     = 5
	feishuOutboxMaxAttempts   = 15
	feishuAdminMessageMaxLen  = 2000
	feishuOutboxCleanupEvery  = time.Hour
	feishuOutboxSentRetention = 90 * 24 * time.Hour
	feishuOutboxDeadRetention = 180 * 24 * time.Hour
)

type feishuOutboxTerminalCleaner interface {
	CleanupTerminal(ctx context.Context, sentBefore, deadBefore time.Time) (int64, error)
}

func (s *FeishuNotificationService) QueueAdminMessage(ctx context.Context, input FeishuAdminMessageInput) (int64, bool, error) {
	if s == nil || s.outboxRepo == nil {
		return 0, false, fmt.Errorf("feishu notification outbox is not configured")
	}
	input.Content = strings.TrimSpace(input.Content)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.UserID <= 0 {
		return 0, false, infraerrors.BadRequest("INVALID_USER_ID", "user_id is required")
	}
	if input.Content == "" || len([]rune(input.Content)) > feishuAdminMessageMaxLen {
		return 0, false, infraerrors.BadRequest("INVALID_FEISHU_MESSAGE", "message must contain 1 to 2000 characters")
	}
	if input.IdempotencyKey == "" {
		return 0, false, infraerrors.BadRequest("IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key is required")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return 0, false, err
	}
	if !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		return 0, false, ErrFeishuNotificationDisabled
	}
	binding, err := s.bindingRepo.GetFeishuNotificationBinding(ctx, input.UserID, cfg.AppID)
	if err != nil {
		return 0, false, err
	}
	if !binding.NotificationEnabled {
		return 0, false, ErrFeishuNotificationDisabled
	}

	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "服务消息"},
			"template": "blue",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": input.Content}},
			s.feishuPanelActionElement(ctx, "打开面板", ""),
		},
	}
	payload, err := json.Marshal(card)
	if err != nil {
		return 0, false, err
	}
	actor := int64(0)
	if input.CreatedBy != nil {
		actor = *input.CreatedBy
	}
	return s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
		DedupeKey: fmt.Sprintf("feishu:admin:%d:%s", actor, input.IdempotencyKey),
		UserID:    input.UserID,
		AppID:     cfg.AppID,
		Category:  FeishuNotificationCategoryAdminService,
		Payload:   payload,
		CreatedBy: input.CreatedBy,
	})
}

func (s *FeishuNotificationService) Start() {
	if s == nil || (s.outboxRepo == nil && s.eventRepo == nil) {
		return
	}
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.workerCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.workerCancel = cancel
	if s.workerID == "" {
		s.workerID = fmt.Sprintf("feishu-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	if s.outboxRepo != nil {
		s.workerWG.Add(1)
		go s.runOutboxWorker(ctx)
	}
	if s.eventRepo != nil && s.outboxRepo != nil {
		s.workerWG.Add(1)
		go s.runFeishuEventWorker(ctx)
	}
	if s.apiKeyRepo != nil && s.outboxRepo != nil {
		s.workerWG.Add(1)
		go s.runAPIKeyExpiryWorker(ctx)
	}
	if s.channelMonitorRepo != nil && s.outboxRepo != nil {
		s.workerWG.Add(1)
		go s.runChannelMonitorNotificationWorker(ctx)
	}
	if s.dailyUsageRepo != nil && s.outboxRepo != nil {
		s.workerWG.Add(1)
		go s.runDailyDigestWorker(ctx)
	}
}

func (s *FeishuNotificationService) Stop() {
	if s == nil {
		return
	}
	s.workerMu.Lock()
	cancel := s.workerCancel
	s.workerCancel = nil
	s.workerMu.Unlock()
	if cancel != nil {
		cancel()
		s.workerWG.Wait()
	}
}

func (s *FeishuNotificationService) runOutboxWorker(ctx context.Context) {
	defer s.workerWG.Done()
	s.processOutboxOnce(ctx)
	ticker := time.NewTicker(feishuOutboxPollInterval)
	cleanupTicker := time.NewTicker(feishuOutboxCleanupEvery)
	defer ticker.Stop()
	defer cleanupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processOutboxOnce(ctx)
		case <-cleanupTicker.C:
			s.cleanupOutboxTerminal(ctx)
		}
	}
}

func (s *FeishuNotificationService) cleanupOutboxTerminal(ctx context.Context) {
	cleaner, ok := s.outboxRepo.(feishuOutboxTerminalCleaner)
	if !ok {
		return
	}
	now := time.Now()
	if _, err := cleaner.CleanupTerminal(ctx, now.Add(-feishuOutboxSentRetention), now.Add(-feishuOutboxDeadRetention)); err != nil && ctx.Err() == nil {
		slog.Warn("feishu outbox cleanup failed", "error", err)
	}
}

func (s *FeishuNotificationService) processOutboxOnce(ctx context.Context) {
	if s == nil || s.outboxRepo == nil {
		return
	}
	items, err := s.outboxRepo.Claim(ctx, s.workerID, feishuOutboxBatchSize, feishuOutboxLease)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("feishu outbox claim failed", "error", err)
		}
		return
	}
	for i := range items {
		s.processOutboxItem(ctx, &items[i])
	}
}

func (s *FeishuNotificationService) processOutboxItem(parent context.Context, item *FeishuNotificationOutboxItem) {
	if item == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()

	cfg, err := s.GetConfig(ctx)
	if err == nil && strings.TrimSpace(cfg.AppID) != strings.TrimSpace(item.AppID) {
		err = fmt.Errorf("notification app changed before delivery")
	}
	if err == nil && item.UserID > 0 && strings.TrimSpace(item.RecipientChatID) == "" {
		for _, category := range FeishuNotificationCategories {
			if item.Category != category {
				continue
			}
			preferences, prefErr := s.GetPreferences(ctx, item.UserID)
			if prefErr != nil {
				err = prefErr
			} else if !preferences[category] {
				finalizeCtx, finalizeCancel := context.WithTimeout(parent, 5*time.Second)
				defer finalizeCancel()
				if markErr := s.outboxRepo.MarkSent(finalizeCtx, item.ID, s.workerID, ""); markErr != nil {
					slog.Warn("feishu outbox mark suppressed failed", "outbox_id", item.ID, "error", markErr)
				}
				return
			}
			break
		}
	}
	var card map[string]any
	if err == nil {
		err = json.Unmarshal(item.Payload, &card)
	}
	messageUUID := fmt.Sprintf("sub2api-feishu-%d", item.ID)
	var messageID string
	if err == nil && strings.TrimSpace(item.RecipientChatID) != "" {
		messageID, err = s.sendInteractiveCardToChatWithUUID(ctx, cfg, item.RecipientChatID, card, messageUUID)
	} else if err == nil && strings.TrimSpace(item.RecipientOpenID) != "" {
		messageID, err = s.sendInteractiveCardToOpenIDWithUUID(ctx, cfg, item.UserID, item.RecipientOpenID, card, messageUUID)
	} else if err == nil && item.Category == FeishuNotificationCategoryBotReply {
		messageID, err = s.sendInteractiveCardWithPreferenceAndUUID(ctx, item.UserID, card, false, messageUUID)
	} else if err == nil {
		messageID, err = s.sendInteractiveCardWithPreferenceAndUUID(ctx, item.UserID, card, true, messageUUID)
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(parent, 5*time.Second)
	defer finalizeCancel()
	if err == nil {
		if markErr := s.outboxRepo.MarkSent(finalizeCtx, item.ID, s.workerID, messageID); markErr != nil {
			slog.Warn("feishu outbox mark sent failed", "outbox_id", item.ID, "error", markErr)
		}
		return
	}

	attempt := item.Attempts + 1
	if attempt >= feishuOutboxMaxAttempts {
		if markErr := s.outboxRepo.MarkDead(finalizeCtx, item.ID, s.workerID, err.Error()); markErr != nil {
			slog.Warn("feishu outbox mark dead failed", "outbox_id", item.ID, "error", markErr)
		}
		return
	}
	backoff := time.Duration(1<<min(attempt, 11)) * time.Second
	if backoff > time.Hour {
		backoff = time.Hour
	}
	if retryErr := s.outboxRepo.Retry(finalizeCtx, item.ID, s.workerID, time.Now().Add(backoff), err.Error()); retryErr != nil {
		slog.Warn("feishu outbox retry schedule failed", "outbox_id", item.ID, "error", retryErr)
	}
}
