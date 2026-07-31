package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

func (s *FeishuNotificationService) renderBotReply(ctx context.Context, binding *FeishuUserIdentityBinding, input string) (string, error) {
	if binding == nil || binding.UserID <= 0 {
		return "", ErrFeishuNotificationNotBound
	}
	command := normalizeFeishuBotCommand(input)
	switch command {
	case "/余额", "/balance", "/概览", "/overview":
		return s.renderFeishuBalance(ctx, binding.UserID)
	case "/订阅", "/subscription", "/subscriptions":
		return s.renderFeishuSubscriptions(ctx, binding.UserID, false)
	case "/额度", "/quota":
		return s.renderFeishuSubscriptions(ctx, binding.UserID, true)
	case "/key", "/keys", "/apikey":
		return s.renderFeishuAPIKeys(ctx, binding.UserID)
	case "/通知", "/notification", "/notifications":
		state := "已关闭"
		if binding.NotificationEnabled {
			state = "已开启"
		}
		return "飞书自动通知：" + state + "\n如需修改，请打开账户面板。", nil
	case "/渠道", "/channel", "/channels":
		return s.renderFeishuChannels(ctx)
	default:
		return "可用命令：\n/概览  /余额  /额度  /订阅  /key  /渠道  /通知\n\n机器人只展示当前绑定账户的脱敏信息。", nil
	}
}

func normalizeFeishuBotCommand(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return "/help"
	}
	return strings.ToLower(fields[0])
}

func (s *FeishuNotificationService) renderFeishuChannels(ctx context.Context) (string, error) {
	if s == nil || s.channelMonitorRepo == nil {
		return "渠道状态暂不可用，请稍后再试。", nil
	}
	monitors, err := s.channelMonitorRepo.ListEnabled(ctx)
	if err != nil {
		return "", err
	}
	if len(monitors) == 0 {
		return "当前没有启用的渠道监控。", nil
	}
	ids := make([]int64, 0, len(monitors))
	for _, monitor := range monitors {
		ids = append(ids, monitor.ID)
	}
	latest, err := s.channelMonitorRepo.ListLatestForMonitorIDs(ctx, ids)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("全局渠道状态")
	for i, monitor := range monitors {
		if i >= 5 {
			fmt.Fprintf(&out, "\n另有 %d 个渠道，请在面板查看。", len(monitors)-i)
			break
		}
		status := "尚未检测"
		for _, item := range latest[monitor.ID] {
			if item != nil && item.Model == monitor.PrimaryModel {
				status = item.Status
				break
			}
		}
		fmt.Fprintf(&out, "\n%s · %s · %s", monitor.Name, monitor.PrimaryModel, status)
	}
	return out.String(), nil
}

func (s *FeishuNotificationService) renderFeishuBalance(ctx context.Context, userID int64) (string, error) {
	if s == nil || s.userRepo == nil {
		return "", fmt.Errorf("user repository is unavailable")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("账户概览\n余额：$%.2f\n并发上限：%d", user.Balance, user.Concurrency), nil
}

func (s *FeishuNotificationService) renderFeishuSubscriptions(ctx context.Context, userID int64, includeQuota bool) (string, error) {
	if s == nil || s.userSubRepo == nil {
		return "", fmt.Errorf("subscription repository is unavailable")
	}
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return "", err
	}
	if len(subs) == 0 {
		return "当前没有有效订阅。", nil
	}
	var out strings.Builder
	if includeQuota {
		out.WriteString("订阅额度")
	} else {
		out.WriteString("有效订阅")
	}
	for i := range subs {
		if i >= 5 {
			fmt.Fprintf(&out, "\n另有 %d 个订阅，请在面板查看。", len(subs)-i)
			break
		}
		sub := &subs[i]
		name := fmt.Sprintf("分组 #%d", sub.GroupID)
		if sub.Group != nil && strings.TrimSpace(sub.Group.Name) != "" {
			name = sub.Group.Name
		}
		fmt.Fprintf(&out, "\n\n%s\n到期：%s", name, sub.ExpiresAt.Local().Format("2006-01-02 15:04"))
		if includeQuota && sub.Group != nil {
			appendFeishuQuotaLine(&out, "日", sub.DailyUsageUSD, sub.Group.DailyLimitUSD)
			appendFeishuQuotaLine(&out, "周", sub.WeeklyUsageUSD, sub.Group.WeeklyLimitUSD)
			appendFeishuQuotaLine(&out, "月", sub.MonthlyUsageUSD, sub.Group.MonthlyLimitUSD)
		}
	}
	return out.String(), nil
}

func appendFeishuQuotaLine(out *strings.Builder, label string, used float64, limit *float64) {
	if limit == nil || *limit <= 0 {
		return
	}
	remaining := *limit - used
	if remaining < 0 {
		remaining = 0
	}
	fmt.Fprintf(out, "\n%s额度：$%.2f / $%.2f，剩余 $%.2f", label, used, *limit, remaining)
}

func (s *FeishuNotificationService) renderFeishuAPIKeys(ctx context.Context, userID int64) (string, error) {
	if s == nil || s.apiKeyRepo == nil {
		return "", fmt.Errorf("api key repository is unavailable")
	}
	keys, page, err := s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{Page: 1, PageSize: 5}, APIKeyListFilters{})
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "当前没有 API Key。", nil
	}
	var out strings.Builder
	out.WriteString("API Key（仅显示后四位）")
	for i := range keys {
		key := &keys[i]
		fmt.Fprintf(&out, "\n\n%s · ****%s\n状态：%s", key.Name, opaqueLastFour(key.Key), key.Status)
		if key.Quota > 0 {
			fmt.Fprintf(&out, "\n总额度：$%.2f / $%.2f", key.QuotaUsed, key.Quota)
		} else {
			out.WriteString("\n总额度：未配置")
		}
		if key.ExpiresAt != nil {
			fmt.Fprintf(&out, "\n到期：%s", key.ExpiresAt.Local().Format("2006-01-02 15:04"))
		}
	}
	if page != nil && page.Total > int64(len(keys)) {
		fmt.Fprintf(&out, "\n\n另有 %d 个 Key，请在面板查看。", page.Total-int64(len(keys)))
	}
	return out.String(), nil
}

func opaqueLastFour(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[len(runes)-4:])
}
