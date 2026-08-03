package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	feishuLanguageChinese = "zh"
	feishuLanguageEnglish = "en"
)

func normalizeFeishuLanguage(value string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "en") {
		return feishuLanguageEnglish
	}
	return feishuLanguageChinese
}

func feishuCommandLanguage(value string) string {
	switch normalizeFeishuBotCommand(value) {
	case "/balance", "/overview", "/subscription", "/subscriptions", "/quota",
		"/key", "/keys", "/apikey", "/daily", "/rank", "/notification",
		"/notifications", "/channel", "/channels", "/help", "/menu", "/request-key", "/requestkey":
		return feishuLanguageEnglish
	default:
		return feishuLanguageChinese
	}
}

func localizeFeishu(zh, en, language string) string {
	if normalizeFeishuLanguage(language) == feishuLanguageEnglish {
		return en
	}
	return zh
}

func isFeishuBotMenuCommand(value string) bool {
	switch normalizeFeishuBotCommand(value) {
	case "/菜单", "/menu", "/帮助", "/help", "help":
		return true
	default:
		return false
	}
}

func isFeishuBotMenuActionCommand(value string) bool {
	command := normalizeFeishuBotCommand(value)
	if isFeishuPersonalSlashCommand(command) || isFeishuAPIKeyRequestCommand(command) {
		return true
	}
	switch command {
	case "/渠道", "/channel", "/channels":
		return true
	default:
		return false
	}
}

func (s *FeishuNotificationService) feishuBotMenuCard(ctx context.Context, language string) map[string]any {
	language = normalizeFeishuLanguage(language)
	command := func(zh, en string) string {
		if language == feishuLanguageEnglish {
			return en
		}
		return zh
	}
	button := func(zhLabel, enLabel, zhCommand, enCommand, buttonType string) map[string]any {
		return map[string]any{
			"tag":  "button",
			"type": buttonType,
			"text": map[string]any{"tag": "plain_text", "content": localizeFeishu(zhLabel, enLabel, language)},
			"value": map[string]any{
				"action":   "bot_command",
				"command":  command(zhCommand, enCommand),
				"language": language,
			},
		}
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": localizeFeishu("Sub2API 账户助手", "Sub2API Account Assistant", language)},
			"template": "blue",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": localizeFeishu("请选择要查询的功能。个人数据只会私聊发送给你。", "Choose an action. Personal data is only sent to you in this direct chat.", language)}},
			map[string]any{"tag": "action", "actions": []any{
				button("账户概览", "Overview", "/概览", "/overview", "primary"),
				button("订阅额度", "Quotas", "/额度", "/quota", "default"),
				button("今日使用", "Today", "/日报", "/daily", "default"),
			}},
			map[string]any{"tag": "action", "actions": []any{
				button("API Keys", "API Keys", "/key", "/keys", "default"),
				button("渠道状态", "Channels", "/渠道", "/channels", "default"),
				button("通知设置", "Notifications", "/通知", "/notifications", "default"),
			}},
			map[string]any{"tag": "action", "actions": []any{
				button("申请 API Key", "Request API Key", "/申请key", "/request-key", "default"),
				map[string]any{"tag": "button", "type": "default", "text": map[string]any{"tag": "plain_text", "content": "中文"}, "value": map[string]any{"action": "bot_menu", "language": feishuLanguageChinese}},
				map[string]any{"tag": "button", "type": "default", "text": map[string]any{"tag": "plain_text", "content": "English"}, "value": map[string]any{"action": "bot_menu", "language": feishuLanguageEnglish}},
			}},
			s.feishuPanelActionElement(ctx, localizeFeishu("打开账户面板", "Open account panel", language), ""),
		},
	}
}

func (s *FeishuNotificationService) handleFeishuP2PChatEntered(ctx context.Context, receipt *FeishuEventReceipt) (string, bool, error) {
	if receipt == nil || receipt.EventType != "im.chat.access_event.bot_p2p_chat_entered_v1" {
		return "", false, nil
	}
	if strings.TrimSpace(receipt.SenderOpenID) == "" {
		return "ignored", true, nil
	}
	reader, ok := s.bindingRepo.(feishuBindingByOpenIDRepository)
	if !ok {
		return "", true, fmt.Errorf("feishu open_id binding lookup is unavailable")
	}
	binding, err := reader.GetFeishuBindingByOpenID(ctx, receipt.AppID, receipt.TenantKey, receipt.SenderOpenID, FeishuIdentityPurposeNotify)
	if errors.Is(err, ErrFeishuNotificationNotBound) {
		card := map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": "账户未绑定 / Account not linked"}, "template": "orange"},
			"elements": []any{
				map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": "请先打开 Sub2API 账户面板完成飞书绑定。\nOpen the Sub2API account panel and link your Feishu identity first."}},
				s.feishuPanelActionElement(ctx, "打开面板 / Open panel", ""),
			},
		}
		status, enqueueErr := s.enqueueFeishuBotCard(ctx, receipt, 0, card)
		return status, true, enqueueErr
	}
	if err != nil {
		return "", true, err
	}
	status, err := s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuBotMenuCard(ctx, feishuLanguageChinese))
	return status, true, err
}

func (s *FeishuNotificationService) renderBotReplyLocalized(ctx context.Context, binding *FeishuUserIdentityBinding, input, language string) (string, error) {
	if binding == nil || binding.UserID <= 0 {
		return "", ErrFeishuNotificationNotBound
	}
	language = normalizeFeishuLanguage(language)
	command := normalizeFeishuBotCommand(input)
	var (
		reply string
		err   error
	)
	switch command {
	case "/余额", "/balance", "/概览", "/overview":
		reply, err = s.renderFeishuBalance(ctx, binding.UserID)
	case "/订阅", "/subscription", "/subscriptions":
		reply, err = s.renderFeishuSubscriptions(ctx, binding.UserID, false)
	case "/额度", "/quota":
		reply, err = s.renderFeishuSubscriptions(ctx, binding.UserID, true)
	case "/key", "/keys", "/apikey":
		reply, err = s.renderFeishuAPIKeys(ctx, binding.UserID)
	case "/日报", "/daily", "/排行", "/rank":
		reply, err = s.renderFeishuDailyUsage(ctx, binding.UserID)
	case "/通知", "/notification", "/notifications":
		state := localizeFeishu("已关闭", "Disabled", language)
		if binding.NotificationEnabled {
			state = localizeFeishu("已开启", "Enabled", language)
		}
		reply = localizeFeishu("飞书自动通知：", "Feishu notifications: ", language) + state + "\n" + localizeFeishu("可在菜单中点击修改。", "Use the menu button to change this setting.", language)
	case "/渠道", "/channel", "/channels":
		reply, err = s.renderFeishuChannels(ctx)
	case "/帮助", "/help", "help", "/菜单", "/menu":
		reply = feishuBotHelpText(language)
	default:
		return s.renderFeishuAssistantReply(ctx, binding, input)
	}
	if err != nil || language != feishuLanguageEnglish {
		return reply, err
	}
	return feishuEnglishReplyReplacer.Replace(reply), nil
}

var feishuEnglishReplyReplacer = strings.NewReplacer(
	"当前没有有效订阅。", "No active subscriptions.",
	"当前没有 API Key。", "No API keys.",
	"当前没有启用的渠道监控。", "No channel monitors are enabled.",
	"渠道状态暂不可用，请稍后再试。", "Channel status is temporarily unavailable. Please try again later.",
	"账户概览", "Account overview",
	"订阅额度", "Subscription quotas",
	"有效订阅", "Active subscriptions",
	"API Key（仅显示后四位）", "API keys (last four characters only)",
	"全局渠道状态", "Global channel status",
	"今日使用", "Today's usage",
	"实际消费：", "Actual spend: ",
	"请求数：", "Requests: ",
	"全站排名：", "Global rank: ",
	"消费占比：", "Spend share: ",
	"余额：", "Balance: ",
	"并发上限：", "Concurrency limit: ",
	"到期：", "Expires: ",
	"日额度：", "Daily quota: ",
	"周额度：", "Weekly quota: ",
	"月额度：", "Monthly quota: ",
	"总额度：", "Total quota: ",
	"状态：", "Status: ",
	"未配置", "Not configured",
	"尚未检测", "Not checked yet",
	"剩余", "remaining",
	"分组 #", "Group #",
	"另有 ", "Another ",
	" 个订阅，请在面板查看。", " subscriptions are available in the account panel.",
	" 个 Key，请在面板查看。", " keys are available in the account panel.",
	" 个渠道，请在面板查看。", " channels are available in the account panel.",
)
