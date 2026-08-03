package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (s *FeishuNotificationService) runFeishuEventWorker(ctx context.Context) {
	defer s.workerWG.Done()
	s.processFeishuEventsOnce(ctx)
	ticker := time.NewTicker(feishuOutboxPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processFeishuEventsOnce(ctx)
		}
	}
}

func (s *FeishuNotificationService) processFeishuEventsOnce(ctx context.Context) {
	items, err := s.eventRepo.Claim(ctx, s.workerID, feishuOutboxBatchSize, feishuOutboxLease)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("feishu event claim failed", "error", err)
		}
		return
	}
	for i := range items {
		s.processFeishuEvent(ctx, &items[i])
	}
}

func (s *FeishuNotificationService) processFeishuEvent(parent context.Context, receipt *FeishuEventReceipt) {
	if receipt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()

	status, err := s.queueFeishuEventReply(ctx, receipt)
	if err == nil {
		if completeErr := s.eventRepo.Complete(ctx, receipt.ID, s.workerID, status); completeErr != nil {
			slog.Warn("feishu event completion failed", "event_receipt_id", receipt.ID, "error", completeErr)
		}
		return
	}
	attempt := receipt.Attempts + 1
	if attempt >= feishuOutboxMaxAttempts {
		if completeErr := s.eventRepo.Complete(ctx, receipt.ID, s.workerID, "failed"); completeErr != nil {
			slog.Warn("feishu event mark failed failed", "event_receipt_id", receipt.ID, "error", completeErr)
		}
		return
	}
	delay := time.Second * time.Duration(1<<min(attempt, 6))
	if retryErr := s.eventRepo.Retry(ctx, receipt.ID, s.workerID, time.Now().Add(delay), err.Error()); retryErr != nil {
		slog.Warn("feishu event retry scheduling failed", "event_receipt_id", receipt.ID, "error", retryErr)
	}
}

func (s *FeishuNotificationService) queueFeishuEventReply(ctx context.Context, receipt *FeishuEventReceipt) (string, error) {
	if receipt.EventType == "card.action.trigger" || receipt.EventType == "card.action.trigger_v1" {
		return s.handleFeishuCardAction(ctx, receipt)
	}
	if receipt.EventType != "im.message.receive_v1" {
		return "ignored", nil
	}
	var payload struct {
		Event struct {
			Sender struct {
				SenderType string `json:"sender_type"`
			} `json:"sender"`
			Message struct {
				ChatType    string `json:"chat_type"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := json.Unmarshal(receipt.Payload, &payload); err != nil {
		return "", err
	}
	if payload.Event.Message.ChatType != "p2p" || payload.Event.Sender.SenderType == "app" {
		return "ignored", nil
	}
	if payload.Event.Message.MessageType != "text" {
		return s.enqueueFeishuBotReply(ctx, receipt, 0, "目前仅支持文本命令。发送 /帮助 查看可用命令。")
	}
	var content struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(payload.Event.Message.Content), &content); err != nil {
		return "", err
	}

	reader, ok := s.bindingRepo.(feishuBindingByOpenIDRepository)
	if !ok {
		return "", fmt.Errorf("feishu open_id binding lookup is unavailable")
	}
	binding, err := reader.GetFeishuBindingByOpenID(ctx, receipt.AppID, receipt.TenantKey, receipt.SenderOpenID, FeishuIdentityPurposeNotify)
	if errors.Is(err, ErrFeishuNotificationNotBound) {
		return s.enqueueFeishuBotReply(ctx, receipt, 0, "尚未绑定本站账户。请先登录站内飞书面板完成绑定。")
	}
	if err != nil {
		return "", err
	}
	input := normalizeFeishuNaturalCommand(content.Text)
	if isFeishuNotificationCommand(input) {
		return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuNotificationToggleCard(ctx, binding.NotificationEnabled))
	}
	if isFeishuAPIKeyRequestCommand(input) {
		card, cardErr := s.buildFeishuAPIKeyRequestCard(ctx, binding)
		if cardErr != nil {
			return "", cardErr
		}
		return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, card)
	}
	reply, err := s.renderBotReply(ctx, binding, input)
	if err != nil {
		return "", err
	}
	return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, reply)
}

func (s *FeishuNotificationService) enqueueFeishuBotReply(ctx context.Context, receipt *FeishuEventReceipt, userID int64, text string) (string, error) {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": "账户助手"}, "template": "blue"},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": strings.TrimSpace(text)}},
			s.feishuPanelActionElement(ctx, "打开面板", ""),
		},
	}
	return s.enqueueFeishuBotCard(ctx, receipt, userID, card)
}

func (s *FeishuNotificationService) enqueueFeishuBotCard(ctx context.Context, receipt *FeishuEventReceipt, userID int64, card map[string]any) (string, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	_, _, err = s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
		DedupeKey: fmt.Sprintf("feishu:event:%s:%s:reply", receipt.AppID, receipt.EventID),
		UserID:    userID, RecipientOpenID: receipt.SenderOpenID, AppID: cfg.AppID,
		Category: FeishuNotificationCategoryBotReply, Payload: encoded,
	})
	if err != nil {
		return "", err
	}
	return "processed", nil
}
