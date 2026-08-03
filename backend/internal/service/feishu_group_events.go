package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var feishuChatLifecycleEvents = map[string]string{
	"im.chat.member.bot.added_v1":   "added",
	"im.chat.member.bot.deleted_v1": "disabled",
	"im.chat.disbanded_v1":          "disabled",
	"im.message.bot_muted_v1":       "disabled",
	"im.chat.member.bot.muted_v1":   "disabled",
	"im.chat.updated_v1":            "updated",
}

func (s *FeishuNotificationService) handleFeishuChatLifecycleEvent(ctx context.Context, receipt *FeishuEventReceipt) (string, bool, error) {
	action, ok := feishuChatLifecycleEvents[receipt.EventType]
	if !ok {
		return "", false, nil
	}
	if s == nil || s.chatRepo == nil {
		return "", true, fmt.Errorf("feishu chat repository is unavailable")
	}
	var payload struct {
		Event struct {
			ChatID string `json:"chat_id"`
			Name   string `json:"name"`
			Chat   struct {
				ChatID string `json:"chat_id"`
				Name   string `json:"name"`
			} `json:"chat"`
		} `json:"event"`
	}
	if err := json.Unmarshal(receipt.Payload, &payload); err != nil {
		return "", true, err
	}
	chatID := firstNonEmpty(payload.Event.ChatID, payload.Event.Chat.ChatID)
	chatName := firstNonEmpty(payload.Event.Name, payload.Event.Chat.Name)
	if strings.TrimSpace(chatID) == "" {
		return "ignored", true, nil
	}
	if action == "disabled" {
		if err := s.chatRepo.DisableChat(ctx, receipt.AppID, receipt.TenantKey, chatID); err != nil {
			return "", true, err
		}
		return "processed", true, nil
	}
	binding, err := s.chatRepo.UpsertPendingChat(ctx, receipt.AppID, receipt.TenantKey, chatID, chatName)
	if err != nil {
		return "", true, err
	}
	if action != "added" || binding.Status == FeishuChatStatusActive {
		return "processed", true, nil
	}
	text := "机器人已加入群聊，但尚未配置用途。\n\n请先在 Sub2API 后台绑定飞书助手管理员，然后由管理员在本群发送：\n/绑定用户群 <分组ID>\n/绑定维护群 <分组ID>\n/绑定管理群\n/绑定通知群"
	status, err := s.enqueueFeishuChatReply(ctx, receipt, chatID, "Sub2API 群助手", text)
	return status, true, err
}

func (s *FeishuNotificationService) handleFeishuGroupMessage(ctx context.Context, receipt *FeishuEventReceipt, chatID, text string) (string, error) {
	if s == nil || s.chatRepo == nil {
		return "", fmt.Errorf("feishu chat repository is unavailable")
	}
	input := normalizeFeishuGroupCommand(text)
	if !strings.HasPrefix(input, "/") {
		return "ignored", nil
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "ignored", nil
	}
	command := strings.ToLower(fields[0])

	var senderBinding *FeishuUserIdentityBinding
	reader, ok := s.bindingRepo.(feishuBindingByOpenIDRepository)
	if ok {
		binding, err := reader.GetFeishuBindingByOpenID(ctx, receipt.AppID, receipt.TenantKey, receipt.SenderOpenID, FeishuIdentityPurposeNotify)
		if err == nil {
			senderBinding = binding
		} else if !errors.Is(err, ErrFeishuNotificationNotBound) {
			return "", err
		}
	}

	chat, err := s.chatRepo.GetChat(ctx, receipt.AppID, receipt.TenantKey, chatID)
	if err != nil && !isFeishuChatNotConfigured(err) {
		return "", err
	}
	if isFeishuChatNotConfigured(err) {
		chat = nil
	}

	if isFeishuChatBindCommand(command) || command == "/解绑群" || command == "/unbind" {
		if senderBinding == nil {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "需要账户绑定", "请先登录 Sub2API，在飞书面板完成账户绑定。")
		}
		allowed, err := s.IsFeishuAssistantAdmin(ctx, senderBinding.UserID)
		if err != nil {
			return "", err
		}
		if !allowed {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "权限不足", "仅后台已授权的飞书助手管理员可以配置群用途。")
		}
		if command == "/解绑群" || command == "/unbind" {
			if err := s.chatRepo.DisableChat(ctx, receipt.AppID, receipt.TenantKey, chatID); err != nil {
				return "", err
			}
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "群助手已停用", "本群绑定已停用，告警与日报也已关闭。可随时重新发送绑定命令启用。")
		}
		return s.configureFeishuChatFromCommand(ctx, receipt, chatID, fields, senderBinding.UserID)
	}

	if command == "/菜单" || command == "/menu" {
		return s.enqueueFeishuChatCard(ctx, receipt, chatID, feishuGroupMenuCard(chat))
	}
	if command == "/帮助" || command == "/help" {
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "Sub2API 群助手帮助", feishuGroupHelpText(chat))
	}
	if chat == nil || chat.Status != FeishuChatStatusActive {
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "群助手尚未配置", "请由后台已授权管理员发送 /绑定用户群、/绑定维护群、/绑定管理群 或 /绑定通知群。")
	}

	switch command {
	case "/群状态", "/group-status":
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "群绑定状态", renderFeishuChatBindingStatus(chat))
	case "/群概览", "/群日报", "/group-usage":
		if chat.Sub2APIGroupID == nil {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "群用量", "此群未关联 Sub2API 分组。")
		}
		text, err := s.renderFeishuGroupToday(ctx, *chat.Sub2APIGroupID)
		if err != nil {
			return "", err
		}
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "分组今日用量", text)
	case "/渠道状态", "/channel-status", "/渠道":
		if chat.Kind != FeishuChatKindOperations && chat.Kind != FeishuChatKindManagement && chat.Kind != FeishuChatKindNotifications {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "功能不可用", "渠道状态仅在维护群、管理群或通知群中提供。")
		}
		text, err := s.renderFeishuChannels(ctx)
		if err != nil {
			return "", err
		}
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "渠道状态", text)
	case "/系统状态", "/system-status":
		if chat.Kind != FeishuChatKindManagement || senderBinding == nil {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "权限不足", "系统状态仅允许管理群中的飞书助手管理员查询。")
		}
		allowed, err := s.IsFeishuAssistantAdmin(ctx, senderBinding.UserID)
		if err != nil {
			return "", err
		}
		if !allowed {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "权限不足", "系统状态仅允许管理群中的飞书助手管理员查询。")
		}
		text, err := s.renderFeishuSystemStatus(ctx)
		if err != nil {
			return "", err
		}
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "系统状态", text)
	}

	if isFeishuPersonalSlashCommand(command) {
		if senderBinding == nil {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "需要账户绑定", "请先登录 Sub2API，在飞书面板完成账户绑定。个人数据不会显示在群聊中。")
		}
		reply, err := s.renderBotReply(ctx, senderBinding, command)
		if err != nil {
			return "", err
		}
		return s.enqueueFeishuBotReply(ctx, receipt, senderBinding.UserID, reply)
	}
	return s.enqueueFeishuChatReply(ctx, receipt, chatID, "未识别的命令", feishuGroupHelpText(chat))
}

func (s *FeishuNotificationService) configureFeishuChatFromCommand(ctx context.Context, receipt *FeishuEventReceipt, chatID string, fields []string, actorID int64) (string, error) {
	command := strings.ToLower(fields[0])
	kind := ""
	switch command {
	case "/绑定用户群", "/绑定使用群", "/bind-user":
		kind = FeishuChatKindUser
	case "/绑定维护群", "/绑定运维群", "/bind-operations":
		kind = FeishuChatKindOperations
	case "/绑定管理群", "/bind-management":
		kind = FeishuChatKindManagement
	case "/绑定通知群", "/绑定告警群", "/bind-notifications":
		kind = FeishuChatKindNotifications
	}
	groupID := int64(0)
	if kind == FeishuChatKindUser || kind == FeishuChatKindOperations {
		if len(fields) < 2 {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "缺少分组 ID", "用法："+fields[0]+" <Sub2API 分组ID>")
		}
		parsed, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || parsed <= 0 {
			return s.enqueueFeishuChatReply(ctx, receipt, chatID, "分组 ID 无效", "请输入有效的 Sub2API 分组数字 ID。")
		}
		groupID = parsed
	}
	input := ConfigureFeishuChatInput{
		AppID: receipt.AppID, TenantKey: receipt.TenantKey, ChatID: chatID,
		Kind: kind, Sub2APIGroupID: groupID, ConfiguredByUserID: actorID,
		IncidentNotificationsEnabled: kind == FeishuChatKindOperations || kind == FeishuChatKindManagement || kind == FeishuChatKindNotifications,
		DailyDigestEnabled:           kind == FeishuChatKindUser || kind == FeishuChatKindOperations,
	}
	if err := s.validateFeishuChatConfiguration(ctx, &input); err != nil {
		return s.enqueueFeishuChatReply(ctx, receipt, chatID, "绑定失败", err.Error())
	}
	binding, err := s.chatRepo.ConfigureChat(ctx, input)
	if err != nil {
		return "", err
	}
	return s.enqueueFeishuChatReply(ctx, receipt, chatID, "群绑定成功", renderFeishuChatBindingStatus(binding)+"\n\n发送 /菜单 查看可用命令。")
}

func (s *FeishuNotificationService) enqueueFeishuChatReply(ctx context.Context, receipt *FeishuEventReceipt, chatID, title, text string) (string, error) {
	card := map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": strings.TrimSpace(title)}, "template": "blue"},
		"elements": []any{map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": strings.TrimSpace(text)}}},
	}
	return s.enqueueFeishuChatCard(ctx, receipt, chatID, card)
}

func (s *FeishuNotificationService) enqueueFeishuChatCard(ctx context.Context, receipt *FeishuEventReceipt, chatID string, card map[string]any) (string, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	_, _, err = s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
		DedupeKey:       fmt.Sprintf("feishu:event:%s:%s:reply", receipt.AppID, receipt.EventID),
		RecipientChatID: strings.TrimSpace(chatID), AppID: cfg.AppID,
		Category: FeishuNotificationCategoryBotReply, Payload: encoded,
	})
	if err != nil {
		return "", err
	}
	return "processed", nil
}

func normalizeFeishuGroupCommand(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	for i, field := range fields {
		if strings.HasPrefix(field, "/") {
			return strings.Join(fields[i:], " ")
		}
	}
	return ""
}

func isFeishuChatBindCommand(command string) bool {
	switch command {
	case "/绑定用户群", "/绑定使用群", "/bind-user",
		"/绑定维护群", "/绑定运维群", "/bind-operations",
		"/绑定管理群", "/bind-management",
		"/绑定通知群", "/绑定告警群", "/bind-notifications":
		return true
	default:
		return false
	}
}

func isFeishuPersonalSlashCommand(command string) bool {
	switch command {
	case "/概览", "/overview", "/余额", "/balance", "/额度", "/quota",
		"/订阅", "/subscription", "/subscriptions", "/key", "/keys", "/apikey",
		"/日报", "/daily", "/排行", "/rank", "/通知", "/notification", "/notifications":
		return true
	default:
		return false
	}
}

func renderFeishuChatBindingStatus(binding *FeishuChatBinding) string {
	if binding == nil {
		return "未配置"
	}
	text := fmt.Sprintf("用途：%s\n状态：%s", feishuChatKindLabel(binding.Kind), binding.Status)
	if binding.Sub2APIGroupID != nil {
		name := strings.TrimSpace(binding.Sub2APIGroupName)
		if name == "" {
			name = fmt.Sprintf("分组 #%d", *binding.Sub2APIGroupID)
		}
		text += "\n关联分组：" + name
	}
	incident := "关闭"
	if binding.IncidentNotificationsEnabled {
		incident = "开启"
	}
	digest := "关闭"
	if binding.DailyDigestEnabled {
		digest = "开启"
	}
	return text + "\n故障通知：" + incident + "\n群日报：" + digest
}

func feishuGroupHelpText(binding *FeishuChatBinding) string {
	base := "个人查询（结果仅私聊本人）：\n/概览  /余额  /额度  /订阅  /key  /日报\n\n群查询：\n/群状态  /群概览  /渠道状态"
	if binding != nil && binding.Kind == FeishuChatKindManagement {
		base += "  /系统状态"
	}
	return base + "\n\n群配置（仅授权管理员）：\n/绑定用户群 <分组ID>\n/绑定维护群 <分组ID>\n/绑定管理群\n/绑定通知群\n/解绑群"
}

func feishuGroupMenuCard(binding *FeishuChatBinding) map[string]any {
	elements := []any{
		map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": renderFeishuChatBindingStatus(binding)}},
	}
	if binding != nil && binding.Status == FeishuChatStatusActive {
		chatID := binding.ChatID
		groupActions := []any{
			feishuGroupMenuButton("群状态", "/群状态", chatID, "default"),
		}
		if binding.Sub2APIGroupID != nil {
			groupActions = append(groupActions, feishuGroupMenuButton("今日群用量", "/群概览", chatID, "primary"))
		}
		if binding.Kind == FeishuChatKindOperations || binding.Kind == FeishuChatKindManagement || binding.Kind == FeishuChatKindNotifications {
			groupActions = append(groupActions, feishuGroupMenuButton("渠道状态", "/渠道状态", chatID, "default"))
		}
		if binding.Kind == FeishuChatKindManagement {
			groupActions = append(groupActions, feishuGroupMenuButton("系统状态", "/系统状态", chatID, "default"))
		}
		elements = append(elements,
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "群功能"}},
			map[string]any{"tag": "action", "actions": groupActions},
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "个人查询（结果仅私聊本人）"}},
			map[string]any{"tag": "action", "actions": []any{
				feishuGroupMenuButton("账户概览", "/概览", chatID, "default"),
				feishuGroupMenuButton("订阅额度", "/额度", chatID, "default"),
				feishuGroupMenuButton("今日使用", "/日报", chatID, "default"),
			}},
		)
	} else {
		elements = append(elements, map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "请由后台已授权管理员先发送群绑定命令。"}})
	}
	return map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": "Sub2API 群助手菜单"}, "template": "blue"},
		"elements": elements,
	}
}

func feishuGroupMenuButton(label, command, chatID, buttonType string) map[string]any {
	return map[string]any{
		"tag": "button", "type": buttonType,
		"text":  map[string]any{"tag": "plain_text", "content": label},
		"value": map[string]any{"action": "group_command", "command": command, "chat_id": chatID},
	}
}

func isFeishuGroupMenuAction(command string) bool {
	if isFeishuPersonalSlashCommand(command) {
		return true
	}
	switch command {
	case "/群状态", "/群概览", "/渠道状态", "/系统状态":
		return true
	default:
		return false
	}
}

func renderFeishuGroupDigestCard(day time.Time, binding FeishuChatBinding, text string) map[string]any {
	return map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": day.Format("2006-01-02") + " 群日报"}, "template": "blue"},
		"elements": []any{map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": text}}},
	}
}
