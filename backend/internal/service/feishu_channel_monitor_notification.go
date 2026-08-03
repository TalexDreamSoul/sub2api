package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	channelMonitorNotificationPoll      = 10 * time.Second
	channelMonitorNotificationLease     = 4 * time.Minute
	channelMonitorNotificationBatchSize = 1
	channelMonitorNotificationMaxTry    = 15
	channelMonitorNotificationCleanup   = 24 * time.Hour
	channelMonitorNotificationRetention = 180 * 24 * time.Hour
)

type feishuChannelRecipientLister interface {
	ListFeishuChannelRecipientUserIDs(ctx context.Context, appID string, afterUserID int64, limit int) ([]int64, error)
}

func (s *FeishuNotificationService) runChannelMonitorNotificationWorker(ctx context.Context) {
	defer s.workerWG.Done()
	s.processChannelMonitorNotificationsOnce(ctx)
	poll := time.NewTicker(channelMonitorNotificationPoll)
	cleanup := time.NewTicker(channelMonitorNotificationCleanup)
	defer poll.Stop()
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			s.processChannelMonitorNotificationsOnce(ctx)
		case <-cleanup.C:
			if _, err := s.channelMonitorRepo.CleanupNotificationEvents(ctx, time.Now().Add(-channelMonitorNotificationRetention)); err != nil && ctx.Err() == nil {
				slog.Warn("channel monitor notification cleanup failed", "error", err)
			}
		}
	}
}

func (s *FeishuNotificationService) processChannelMonitorNotificationsOnce(ctx context.Context) {
	events, err := s.channelMonitorRepo.ClaimNotificationEvents(ctx, s.workerID, channelMonitorNotificationBatchSize, channelMonitorNotificationLease)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("channel monitor notification claim failed", "error", err)
		}
		return
	}
	for i := range events {
		s.processChannelMonitorNotificationEvent(ctx, &events[i])
	}
}

func (s *FeishuNotificationService) processChannelMonitorNotificationEvent(parent context.Context, event *ChannelMonitorNotificationEvent) {
	if event == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	err := s.queueChannelMonitorEvent(ctx, event)
	finalizeCtx, finalizeCancel := context.WithTimeout(parent, 5*time.Second)
	defer finalizeCancel()
	if err == nil {
		if markErr := s.channelMonitorRepo.MarkNotificationEventSent(finalizeCtx, event.ID, s.workerID); markErr != nil {
			slog.Warn("channel monitor notification mark sent failed", "event_id", event.ID, "error", markErr)
		}
		return
	}
	attempt := event.Attempts + 1
	dead := attempt >= channelMonitorNotificationMaxTry
	backoff := time.Duration(1<<min(attempt, 11)) * time.Second
	if backoff > time.Hour {
		backoff = time.Hour
	}
	if retryErr := s.channelMonitorRepo.RetryNotificationEvent(finalizeCtx, event.ID, s.workerID, time.Now().Add(backoff), err.Error(), dead); retryErr != nil {
		slog.Warn("channel monitor notification retry failed", "event_id", event.ID, "error", retryErr)
	}
}

func (s *FeishuNotificationService) queueChannelMonitorEvent(ctx context.Context, event *ChannelMonitorNotificationEvent) error {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		return nil
	}
	card := channelMonitorNotificationCard(event)
	businessKey := fmt.Sprintf("channel-monitor:%d:%d:%s", event.MonitorID, event.IncidentVersion, event.EventKind)
	lister, ok := s.bindingRepo.(feishuChannelRecipientLister)
	if !ok {
		return fmt.Errorf("feishu channel recipient listing is unavailable")
	}
	var afterUserID int64
	for {
		userIDs, err := lister.ListFeishuChannelRecipientUserIDs(ctx, cfg.AppID, afterUserID, 500)
		if err != nil {
			return err
		}
		for _, userID := range userIDs {
			if err := s.queueNotificationCard(ctx, userID, "channel", businessKey, card); err != nil &&
				!errors.Is(err, ErrFeishuNotificationDisabled) && !errors.Is(err, ErrFeishuNotificationNotBound) {
				return err
			}
			afterUserID = userID
		}
		if len(userIDs) < 500 {
			break
		}
	}
	if s.chatRepo == nil {
		return nil
	}
	chats, err := s.chatRepo.ListActiveChats(ctx, []string{FeishuChatKindOperations, FeishuChatKindManagement, FeishuChatKindNotifications})
	if err != nil {
		return err
	}
	payload, err := marshalFeishuCard(card)
	if err != nil {
		return err
	}
	for i := range chats {
		chat := &chats[i]
		if !chat.IncidentNotificationsEnabled {
			continue
		}
		if _, _, err := s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
			DedupeKey:       fmt.Sprintf("feishu:chat:%s:%s:%s", chat.ChatID, businessKey, event.EventKind),
			OrderingKey:     "feishu:chat:" + chat.ChatID,
			RecipientChatID: chat.ChatID, AppID: cfg.AppID,
			Category: "channel", Payload: payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func channelMonitorNotificationCard(event *ChannelMonitorNotificationEvent) map[string]any {
	title := "渠道故障通知"
	template := "red"
	statusText := "连续检测失败，渠道可能暂时不可用。"
	if event.EventKind == "recovery" {
		title = "渠道恢复通知"
		template = "green"
		statusText = "渠道检测已恢复正常。"
	}
	fields := []any{
		map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": "渠道\n" + event.MonitorName}},
		map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": "模型\n" + event.Model}},
		map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": "状态\n" + event.ObservedStatus}},
		map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": "检测时间\n" + event.CheckedAt.Local().Format("2006-01-02 15:04:05")}},
	}
	if event.LatencyMs != nil {
		fields = append(fields, map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("延迟\n%d ms", *event.LatencyMs)}})
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": title}, "template": template},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": statusText}},
			map[string]any{"tag": "div", "fields": fields},
		},
	}
}
