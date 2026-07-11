package service

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	openAIQuotaNotifyEnabledKey = "openai_quota_notify_enabled"
	openAIQuotaNotifyRulesKey   = "openai_quota_notify_rules"
	maxOpenAIQuotaNotifyRules   = 10
)

var openAIQuotaWindowLabels = map[string]string{
	"5h": "5 小时窗口 / 5h",
	"7d": "7 天窗口 / 7d",
}

type OpenAIQuotaNotifyRule struct {
	Window           string `json:"window"`
	RemainingPercent int    `json:"remaining_percent"`
}

type accountQuotaNotificationClaimer interface {
	ClaimQuotaNotification(ctx context.Context, accountID int64, stateKey, cycleKey string) (bool, error)
}

func parseOpenAIQuotaNotifyRules(raw any) ([]OpenAIQuotaNotifyRule, error) {
	if raw == nil {
		return nil, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAI quota notify rules: %w", err)
	}
	var rules []OpenAIQuotaNotifyRule
	if err := json.Unmarshal(payload, &rules); err != nil {
		return nil, fmt.Errorf("invalid OpenAI quota notify rules: %w", err)
	}
	return rules, nil
}

func ValidateOpenAIQuotaNotifyConfig(platform, accountType string, extra map[string]any) error {
	if extra == nil {
		return nil
	}
	enabled, _ := extra[openAIQuotaNotifyEnabledKey].(bool)
	rawRules, hasRules := extra[openAIQuotaNotifyRulesKey]
	if !enabled && !hasRules {
		return nil
	}
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
		if enabled {
			return fmt.Errorf("OpenAI quota notifications are only supported for OpenAI OAuth accounts")
		}
		delete(extra, openAIQuotaNotifyRulesKey)
		return nil
	}
	rules, err := parseOpenAIQuotaNotifyRules(rawRules)
	if err != nil {
		return err
	}
	if len(rules) > maxOpenAIQuotaNotifyRules {
		return fmt.Errorf("OpenAI quota notify rules must not exceed %d", maxOpenAIQuotaNotifyRules)
	}
	seen := make(map[string]struct{}, len(rules))
	for i := range rules {
		rules[i].Window = strings.TrimSpace(rules[i].Window)
		if _, ok := openAIQuotaWindowLabels[rules[i].Window]; !ok {
			return fmt.Errorf("OpenAI quota notify window must be 5h or 7d")
		}
		if rules[i].RemainingPercent <= 0 || rules[i].RemainingPercent >= 100 {
			return fmt.Errorf("OpenAI quota remaining percentage must be between 1 and 99")
		}
		key := fmt.Sprintf("%s:%d", rules[i].Window, rules[i].RemainingPercent)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate OpenAI quota notify rule: %s", key)
		}
		seen[key] = struct{}{}
	}
	if enabled && len(rules) == 0 {
		return fmt.Errorf("at least one OpenAI quota notify rule is required when notifications are enabled")
	}
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Window == rules[j].Window {
			return rules[i].RemainingPercent > rules[j].RemainingPercent
		}
		return rules[i].Window < rules[j].Window
	})
	extra[openAIQuotaNotifyRulesKey] = rules
	return nil
}

func (a *Account) OpenAIQuotaNotifyConfig() (bool, []OpenAIQuotaNotifyRule) {
	if a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.Extra == nil {
		return false, nil
	}
	enabled, _ := a.Extra[openAIQuotaNotifyEnabledKey].(bool)
	rules, err := parseOpenAIQuotaNotifyRules(a.Extra[openAIQuotaNotifyRulesKey])
	if err != nil {
		return false, nil
	}
	return enabled, rules
}

func openAIQuotaWindowSnapshot(extra map[string]any, window string) (float64, string, bool) {
	usedKey := "codex_" + window + "_used_percent"
	resetKey := "codex_" + window + "_reset_at"
	used, ok := extraNumber(extra[usedKey])
	if !ok {
		return 0, "", false
	}
	resetAt, _ := extra[resetKey].(string)
	resetAt = strings.TrimSpace(resetAt)
	resetTime, err := time.Parse(time.RFC3339, resetAt)
	if err != nil || !resetTime.After(time.Now()) {
		return 0, "", false
	}
	return used, resetTime.UTC().Truncate(time.Minute).Format(time.RFC3339), true
}

func extraNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func (s *BalanceNotifyService) CheckPersistedOpenAIQuotaSnapshot(ctx context.Context, accountID int64) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		slog.Warn("load account for OpenAI quota notification failed", "account_id", accountID, "error", err)
		return
	}
	s.CheckOpenAIQuotaSnapshot(ctx, account)
}

func (s *BalanceNotifyService) CheckOpenAIQuotaSnapshot(ctx context.Context, account *Account) {
	if s == nil || account == nil || s.settingRepo == nil || s.emailService == nil {
		return
	}
	if !s.isAccountQuotaNotifyEnabled(ctx) {
		return
	}
	enabled, rules := account.OpenAIQuotaNotifyConfig()
	if !enabled || len(rules) == 0 {
		return
	}
	recipients := s.getAccountQuotaNotifyEmails(ctx)
	if len(recipients) == 0 {
		return
	}
	claimer, ok := s.accountRepo.(accountQuotaNotificationClaimer)
	if !ok {
		slog.Warn("account repository does not support quota notification claims")
		return
	}
	siteName := s.getSiteName(ctx)
	for _, rule := range rules {
		used, resetAt, ok := openAIQuotaWindowSnapshot(account.Extra, rule.Window)
		if !ok {
			continue
		}
		remaining := 100 - used
		if remaining < 0 {
			remaining = 0
		}
		if remaining > float64(rule.RemainingPercent) {
			continue
		}
		stateKey := fmt.Sprintf("quota_notify_state_openai_%s_%d", rule.Window, rule.RemainingPercent)
		claimed, err := claimer.ClaimQuotaNotification(ctx, account.ID, stateKey, resetAt)
		if err != nil {
			slog.Warn("claim OpenAI quota notification failed", "account_id", account.ID, "window", rule.Window, "threshold", rule.RemainingPercent, "error", err)
			continue
		}
		if !claimed {
			continue
		}
		go s.sendOpenAIQuotaAlertEmails(recipients, account, rule, used, remaining, resetAt, siteName)
	}
}

func (s *BalanceNotifyService) sendOpenAIQuotaAlertEmails(recipients []string, account *Account, rule OpenAIQuotaNotifyRule, used, remaining float64, resetAt, siteName string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("panic in OpenAI quota notification", "recover", recovered)
		}
	}()
	dimLabel := openAIQuotaWindowLabels[rule.Window]
	thresholdDisplay := fmt.Sprintf("%d%%", rule.RemainingPercent)
	if s.notificationEmailService != nil {
		fallbackRecipients := make([]string, 0, len(recipients))
		for _, to := range recipients {
			ctx, cancel := context.WithTimeout(context.Background(), emailSendTimeout)
			err := s.notificationEmailService.Send(ctx, NotificationEmailSendInput{
				Event:          NotificationEmailEventAccountQuotaAlert,
				RecipientEmail: to,
				RecipientName:  emailRecipientName(to),
				SourceType:     "account_quota",
				SourceID:       fmt.Sprintf("%d-openai-%s-%d", account.ID, rule.Window, rule.RemainingPercent),
				ReminderKey:    resetAt,
				Variables: map[string]string{
					"account_id":      strconv.FormatInt(account.ID, 10),
					"account_name":    account.Name,
					"platform":        account.Platform,
					"quota_dimension": dimLabel,
					"quota_used":      fmt.Sprintf("%.0f%%", used),
					"quota_limit":     "100%",
					"quota_remaining": fmt.Sprintf("%.0f%%", remaining),
					"quota_threshold": thresholdDisplay,
				},
			})
			cancel()
			if err != nil {
				if shouldFallbackNotificationEmail(err) {
					fallbackRecipients = append(fallbackRecipients, to)
				} else {
					slog.Warn("OpenAI quota notification delivery failed", "to", to, "account_id", account.ID, "window", rule.Window, "error", err)
				}
			}
		}
		if len(fallbackRecipients) == 0 {
			return
		}
		recipients = fallbackRecipients
	}
	subject := fmt.Sprintf("[%s] OpenAI 配额提醒 - %s", sanitizeEmailHeader(siteName), sanitizeEmailHeader(account.Name))
	body := fmt.Sprintf(`<!doctype html><html><body><h2>%s</h2><p>账号 <strong>%s</strong> 的 %s 剩余配额已不高于 %s。</p><table><tr><td>已用</td><td>%.0f%%</td></tr><tr><td>剩余</td><td>%.0f%%</td></tr><tr><td>窗口重置</td><td>%s</td></tr></table></body></html>`, html.EscapeString(siteName), html.EscapeString(account.Name), html.EscapeString(dimLabel), thresholdDisplay, used, remaining, html.EscapeString(resetAt))
	s.sendEmails(recipients, subject, body, "account", account.Name, "window", rule.Window)
}
