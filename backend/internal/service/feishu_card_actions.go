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

func (s *FeishuNotificationService) feishuNotificationToggleCard(ctx context.Context, enabled bool, languages ...string) map[string]any {
	language := feishuLanguageChinese
	if len(languages) > 0 {
		language = normalizeFeishuLanguage(languages[0])
	}
	state, command, buttonType := localizeFeishu("已关闭", "Disabled", language), localizeFeishu("开启自动通知", "Enable notifications", language), "primary"
	if enabled {
		state, command, buttonType = localizeFeishu("已开启", "Enabled", language), localizeFeishu("关闭自动通知", "Disable notifications", language), "danger"
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": localizeFeishu("通知设置", "Notification settings", language)}, "template": "blue"},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": localizeFeishu("飞书自动通知：", "Feishu notifications: ", language) + state}},
			map[string]any{"tag": "action", "actions": []any{map[string]any{
				"tag": "button", "type": buttonType,
				"text":  map[string]any{"tag": "plain_text", "content": command},
				"value": map[string]any{"action": "notification_toggle", "enabled": !enabled, "language": language},
				"confirm": map[string]any{
					"title": map[string]any{"tag": "plain_text", "content": localizeFeishu("确认修改通知设置", "Confirm notification change", language)},
					"text":  map[string]any{"tag": "plain_text", "content": localizeFeishu("确认", "Confirm: ", language) + command + "?"},
				},
			}}},
			s.feishuPanelActionElement(ctx, localizeFeishu("打开面板", "Open panel", language), ""),
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
	action := strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["action"]))
	if action == "group_command" {
		command := normalizeFeishuBotCommand(strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["command"])))
		chatID := strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["chat_id"]))
		if chatID == "" || !isFeishuGroupMenuAction(command) {
			return "ignored", nil
		}
		return s.handleFeishuGroupMessage(ctx, receipt, chatID, command)
	}
	if action != "notification_toggle" && action != "api_key_request" && action != "bot_menu" && action != "bot_command" {
		return "ignored", nil
	}
	reader, ok := s.bindingRepo.(feishuBindingByOpenIDRepository)
	if !ok {
		return "", fmt.Errorf("feishu open_id binding lookup is unavailable")
	}
	binding, err := reader.GetFeishuBindingByOpenID(ctx, receipt.AppID, receipt.TenantKey, receipt.SenderOpenID, FeishuIdentityPurposeNotify)
	if errors.Is(err, ErrFeishuNotificationNotBound) {
		return s.enqueueFeishuBotReply(ctx, receipt, 0, "尚未绑定本站账户，无法执行该操作。")
	}
	if err != nil {
		return "", err
	}
	language := normalizeFeishuLanguage(strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["language"])))
	if action == "bot_menu" {
		return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuBotMenuCard(ctx, language))
	}
	if action == "bot_command" {
		command := normalizeFeishuBotCommand(strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["command"])))
		if !isFeishuBotMenuActionCommand(command) {
			return "ignored", nil
		}
		if isFeishuNotificationCommand(command) {
			return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuNotificationToggleCard(ctx, binding.NotificationEnabled, language))
		}
		if isFeishuAPIKeyRequestCommand(command) {
			card, cardErr := s.buildFeishuAPIKeyRequestCard(ctx, binding, language)
			if cardErr != nil {
				return "", cardErr
			}
			return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, card)
		}
		reply, renderErr := s.renderBotReplyLocalized(ctx, binding, command, language)
		if renderErr != nil {
			return "", renderErr
		}
		return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, reply)
	}
	if action == "api_key_request" {
		groupID, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(payload.Event.Action.Value["group_id"])), 10, 64)
		if err != nil || groupID <= 0 {
			return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, "API Key 申请参数无效。")
		}
		return s.handleFeishuAPIKeyRequestAction(ctx, receipt, binding, groupID, language)
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
	status, err := s.SetEnabled(ctx, binding.UserID, enabled)
	if err != nil {
		return "", err
	}
	return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuNotificationToggleCard(ctx, status.NotificationEnabled, language))
}
