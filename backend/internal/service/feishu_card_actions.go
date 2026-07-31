package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func isFeishuNotificationCommand(value string) bool {
	switch normalizeFeishuBotCommand(value) {
	case "/通知", "/notification", "/notifications":
		return true
	default:
		return false
	}
}

func (s *FeishuNotificationService) feishuNotificationToggleCard(ctx context.Context, enabled bool) map[string]any {
	state, command, buttonType := "已关闭", "开启自动通知", "primary"
	if enabled {
		state, command, buttonType = "已开启", "关闭自动通知", "danger"
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": "通知设置"}, "template": "blue"},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "飞书自动通知：" + state}},
			map[string]any{"tag": "action", "actions": []any{map[string]any{
				"tag": "button", "type": buttonType,
				"text":  map[string]any{"tag": "plain_text", "content": command},
				"value": map[string]any{"action": "notification_toggle", "enabled": !enabled},
				"confirm": map[string]any{
					"title": map[string]any{"tag": "plain_text", "content": "确认修改通知设置"},
					"text":  map[string]any{"tag": "plain_text", "content": "确认" + command + "？"},
				},
			}}},
			s.feishuPanelActionElement(ctx, "打开面板", ""),
		},
	}
}

func (s *FeishuNotificationService) handleFeishuCardAction(ctx context.Context, receipt *FeishuEventReceipt) (string, error) {
	var payload struct {
		Event struct {
			Action struct {
				Value map[string]any `json:"value"`
			} `json:"action"`
		} `json:"event"`
	}
	if err := json.Unmarshal(receipt.Payload, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["action"])) != "notification_toggle" {
		return "ignored", nil
	}
	var enabled bool
	switch value := payload.Event.Action.Value["enabled"].(type) {
	case bool:
		enabled = value
	case string:
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return "ignored", nil
		}
		enabled = parsed
	default:
		return "ignored", nil
	}
	reader, ok := s.bindingRepo.(feishuBindingByOpenIDRepository)
	if !ok {
		return "", fmt.Errorf("feishu open_id binding lookup is unavailable")
	}
	binding, err := reader.GetFeishuBindingByOpenID(ctx, receipt.AppID, receipt.TenantKey, receipt.SenderOpenID, FeishuIdentityPurposeNotify)
	if errors.Is(err, ErrFeishuNotificationNotBound) {
		return s.enqueueFeishuBotReply(ctx, receipt, 0, "尚未绑定本站账户，无法修改通知设置。")
	}
	if err != nil {
		return "", err
	}
	status, err := s.SetEnabled(ctx, binding.UserID, enabled)
	if err != nil {
		return "", err
	}
	return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuNotificationToggleCard(ctx, status.NotificationEnabled))
}
