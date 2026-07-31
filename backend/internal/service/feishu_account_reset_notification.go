package service

import (
	"context"
	"fmt"
)

func (s *FeishuNotificationService) QueueAccountResetRefund(ctx context.Context, operationID int64, refund *AccountResetRefundResult) error {
	if refund == nil || operationID <= 0 {
		return nil
	}
	type totals struct{ daily, weekly, monthly float64 }
	byUser := make(map[int64]totals)
	for _, item := range refund.Adjustments {
		value := byUser[item.UserID]
		value.daily += item.DailyRefunded
		value.weekly += item.WeeklyRefunded
		value.monthly += item.MonthlyRefunded
		byUser[item.UserID] = value
	}
	var lastErr error
	for userID, value := range byUser {
		if value.daily <= 0 && value.weekly <= 0 && value.monthly <= 0 {
			continue
		}
		card := map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": "订阅用量已返还"}, "template": "green"},
			"elements": []any{
				map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "账号重置后的订阅用量调整已完成。"}},
				map[string]any{"tag": "div", "fields": []any{
					map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("日窗口\n$%.4f", value.daily)}},
					map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("周窗口\n$%.4f", value.weekly)}},
					map[string]any{"is_short": true, "text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("月窗口\n$%.4f", value.monthly)}},
				}},
				s.feishuPanelActionElement(ctx, "查看订阅", ""),
			},
		}
		if err := s.queueNotificationCard(ctx, userID, "quota", fmt.Sprintf("account-reset-refund:%d:%d", operationID, userID), card); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
