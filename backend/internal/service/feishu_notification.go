package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/imroc/req/v3"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"
)

const (
	FeishuIdentityPurposeNotify = "notify"
	FeishuIdentityPurposePanel  = "panel"

	defaultFeishuNotifyTokenURL   = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
	defaultFeishuNotifyMessageURL = "https://open.feishu.cn/open-apis/im/v1/messages"
	defaultFeishuPanelPath        = "/feishu/panel"
)

var (
	ErrFeishuNotificationNotBound = infraerrors.NotFound("FEISHU_NOTIFICATION_NOT_BOUND", "feishu notification is not bound")
	ErrFeishuNotificationConflict = infraerrors.Conflict("FEISHU_NOTIFICATION_CONFLICT", "feishu identity is already bound to another user")
	ErrFeishuNotificationDisabled = infraerrors.Forbidden("FEISHU_NOTIFICATION_DISABLED", "feishu notification is disabled")
)

type FeishuNotificationConfig struct {
	Enabled           bool
	AppID             string
	AppSecret         string
	TokenURL          string
	MessageURL        string
	PanelURL          string
	VerificationToken string
	EncryptKey        string
}

type FeishuUserIdentityBinding struct {
	UserID              int64
	AppID               string
	TenantKey           string
	OpenID              string
	UnionID             string
	Purpose             string
	NotificationEnabled bool
	Metadata            map[string]any
	BoundAt             time.Time
	LastSeenAt          time.Time
}

type UpsertFeishuUserIdentityBindingInput struct {
	UserID              int64
	AppID               string
	TenantKey           string
	OpenID              string
	UnionID             string
	Purpose             string
	NotificationEnabled bool
	Metadata            map[string]any
}

type FeishuUserIdentityRepository interface {
	UpsertFeishuUserIdentityBinding(ctx context.Context, input UpsertFeishuUserIdentityBindingInput) (*FeishuUserIdentityBinding, error)
	GetFeishuNotificationBinding(ctx context.Context, userID int64, appID string) (*FeishuUserIdentityBinding, error)
	GetFeishuBindingByUnionID(ctx context.Context, appID, tenantKey, unionID, purpose string) (*FeishuUserIdentityBinding, error)
	ListFeishuBindingsByUser(ctx context.Context, userID int64) ([]FeishuUserIdentityBinding, error)
	SetFeishuNotificationEnabled(ctx context.Context, userID int64, appID string, enabled bool) (*FeishuUserIdentityBinding, error)
	DeleteFeishuNotificationBinding(ctx context.Context, userID int64, appID string) error
}

type feishuBindingByOpenIDRepository interface {
	GetFeishuBindingByOpenID(ctx context.Context, appID, tenantKey, openID, purpose string) (*FeishuUserIdentityBinding, error)
}

type FeishuNotificationStatus struct {
	Bound               bool            `json:"bound"`
	Enabled             bool            `json:"enabled"`
	AppID               string          `json:"app_id,omitempty"`
	TenantKey           string          `json:"tenant_key,omitempty"`
	UnionIDHint         string          `json:"union_id_hint,omitempty"`
	OpenIDHint          string          `json:"open_id_hint,omitempty"`
	BindStartPath       string          `json:"bind_start_path,omitempty"`
	PanelURL            string          `json:"panel_url,omitempty"`
	CanOpenPanel        bool            `json:"can_open_panel"`
	NotificationEnabled bool            `json:"notification_enabled"`
	Preferences         map[string]bool `json:"preferences"`
}

type FeishuBalanceLowNotification struct {
	UserID      int64
	UserName    string
	UserEmail   string
	Balance     float64
	Threshold   float64
	SiteName    string
	RechargeURL string
}

type FeishuSubscriptionExpiryNotification struct {
	UserID            int64
	SubscriptionID    int64
	RecipientName     string
	GroupName         string
	ExpiresAt         time.Time
	DaysRemaining     int
	SourceReminderKey string
}

type FeishuContentModerationBanNotification struct {
	SourceEventID  int64
	UserID         int64
	UserName       string
	UserEmail      string
	GroupName      string
	Category       string
	Score          float64
	ViolationCount int
	BanThreshold   int
	BanDurationMin int
}

type FeishuContentModerationViolationNotification struct {
	SourceEventID  int64
	UserID         int64
	UserName       string
	UserEmail      string
	GroupName      string
	Category       string
	Score          float64
	ViolationCount int
	BanThreshold   int
}

type FeishuNotificationService struct {
	settingRepo        SettingRepository
	bindingRepo        FeishuUserIdentityRepository
	outboxRepo         FeishuNotificationOutboxRepository
	eventRepo          FeishuEventReceiptRepository
	userRepo           UserRepository
	userSubRepo        UserSubscriptionRepository
	apiKeyRepo         APIKeyRepository
	preferenceRepo     NotificationPreferenceRepository
	channelMonitorRepo ChannelMonitorRepository
	apiKeyRequestRepo  FeishuAPIKeyRequestRepository
	apiKeyService      *APIKeyService
	dailyUsageRepo     feishuDailyDigestUsageReader
	dailyDigestMu      sync.Mutex
	dailyDigestDate    string

	tokenMu            sync.RWMutex
	tokenCacheKey      string
	tokenValue         string
	tokenExpiresAt     time.Time
	tokenFlight        singleflight.Group
	tokenFetchObserver func()

	workerMu     sync.Mutex
	workerCancel context.CancelFunc
	workerWG     sync.WaitGroup
	workerID     string
}

func NewFeishuNotificationService(settingRepo SettingRepository, bindingRepo FeishuUserIdentityRepository, outboxRepos ...FeishuNotificationOutboxRepository) *FeishuNotificationService {
	svc := &FeishuNotificationService{settingRepo: settingRepo, bindingRepo: bindingRepo}
	if len(outboxRepos) > 0 {
		svc.outboxRepo = outboxRepos[0]
	}
	return svc
}

func (s *FeishuNotificationService) GetConfig(ctx context.Context) (FeishuNotificationConfig, error) {
	cfg := FeishuNotificationConfig{
		TokenURL:   defaultFeishuNotifyTokenURL,
		MessageURL: defaultFeishuNotifyMessageURL,
		PanelURL:   defaultFeishuPanelPath,
	}
	if s == nil || s.settingRepo == nil {
		return cfg, nil
	}
	settings, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyFeishuNotifyEnabled,
		SettingKeyFeishuNotifyAppID,
		SettingKeyFeishuNotifyAppSecret,
		SettingKeyFeishuNotifyTokenURL,
		SettingKeyFeishuNotifyMessageURL,
		SettingKeyFeishuNotifyPanelURL,
		SettingKeyFeishuNotifyVerificationToken,
		SettingKeyFeishuNotifyEncryptKey,
	})
	if err != nil {
		return cfg, err
	}
	cfg.Enabled = strings.TrimSpace(settings[SettingKeyFeishuNotifyEnabled]) == "true"
	cfg.AppID = strings.TrimSpace(settings[SettingKeyFeishuNotifyAppID])
	cfg.AppSecret = strings.TrimSpace(settings[SettingKeyFeishuNotifyAppSecret])
	cfg.TokenURL = firstNonEmpty(settings[SettingKeyFeishuNotifyTokenURL], defaultFeishuNotifyTokenURL)
	cfg.MessageURL = firstNonEmpty(settings[SettingKeyFeishuNotifyMessageURL], defaultFeishuNotifyMessageURL)
	cfg.PanelURL = firstNonEmpty(settings[SettingKeyFeishuNotifyPanelURL], defaultFeishuPanelPath)
	cfg.VerificationToken = strings.TrimSpace(settings[SettingKeyFeishuNotifyVerificationToken])
	cfg.EncryptKey = strings.TrimSpace(settings[SettingKeyFeishuNotifyEncryptKey])
	return cfg, nil
}

func (s *FeishuNotificationService) GetStatus(ctx context.Context, userID int64) (FeishuNotificationStatus, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return FeishuNotificationStatus{}, err
	}
	status := FeishuNotificationStatus{
		AppID:         cfg.AppID,
		PanelURL:      cfg.PanelURL,
		CanOpenPanel:  cfg.Enabled && cfg.AppID != "" && cfg.PanelURL != "",
		BindStartPath: "/api/v1/auth/oauth/feishu/notify/bind/start",
	}
	if s == nil || s.bindingRepo == nil || userID <= 0 || cfg.AppID == "" {
		return status, nil
	}
	binding, err := s.bindingRepo.GetFeishuNotificationBinding(ctx, userID, cfg.AppID)
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(ErrFeishuNotificationNotBound) {
			return status, nil
		}
		return status, err
	}
	status.Bound = true
	status.Enabled = binding.NotificationEnabled
	status.NotificationEnabled = binding.NotificationEnabled
	status.TenantKey = binding.TenantKey
	status.UnionIDHint = maskOpaqueIdentity(binding.UnionID)
	status.OpenIDHint = maskOpaqueIdentity(binding.OpenID)
	if preferences, prefErr := s.GetPreferences(ctx, userID); prefErr == nil {
		status.Preferences = preferences
	}
	return status, nil
}

func (s *FeishuNotificationService) SetEnabled(ctx context.Context, userID int64, enabled bool) (FeishuNotificationStatus, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return FeishuNotificationStatus{}, err
	}
	if cfg.AppID == "" || s == nil || s.bindingRepo == nil {
		return FeishuNotificationStatus{}, ErrFeishuNotificationNotBound
	}
	if _, err := s.bindingRepo.SetFeishuNotificationEnabled(ctx, userID, cfg.AppID, enabled); err != nil {
		return FeishuNotificationStatus{}, err
	}
	return s.GetStatus(ctx, userID)
}

func (s *FeishuNotificationService) GetPreferences(ctx context.Context, userID int64) (map[string]bool, error) {
	defaults := make(map[string]bool, len(FeishuNotificationCategories))
	for _, category := range FeishuNotificationCategories {
		defaults[category] = true
	}
	if s == nil || s.preferenceRepo == nil {
		return defaults, nil
	}
	return s.preferenceRepo.Get(ctx, userID, "feishu", FeishuNotificationCategories)
}

func (s *FeishuNotificationService) SetPreferences(ctx context.Context, userID int64, preferences map[string]bool) (map[string]bool, error) {
	if s == nil || s.preferenceRepo == nil {
		return nil, fmt.Errorf("notification preference repository is unavailable")
	}
	allowed := make(map[string]struct{}, len(FeishuNotificationCategories))
	for _, category := range FeishuNotificationCategories {
		allowed[category] = struct{}{}
	}
	for category := range preferences {
		if _, ok := allowed[category]; !ok {
			return nil, infraerrors.BadRequest("INVALID_NOTIFICATION_CATEGORY", "invalid notification category")
		}
	}
	if err := s.preferenceRepo.Set(ctx, userID, "feishu", preferences); err != nil {
		return nil, err
	}
	return s.GetPreferences(ctx, userID)
}

func (s *FeishuNotificationService) UpsertNotifyBinding(ctx context.Context, input UpsertFeishuUserIdentityBindingInput) (*FeishuUserIdentityBinding, error) {
	if s == nil || s.bindingRepo == nil {
		return nil, ErrFeishuNotificationDisabled
	}
	input.Purpose = FeishuIdentityPurposeNotify
	input.NotificationEnabled = true
	return s.bindingRepo.UpsertFeishuUserIdentityBinding(ctx, input)
}

func (s *FeishuNotificationService) SendBalanceLow(ctx context.Context, input FeishuBalanceLowNotification) error {
	displayName := firstNonEmpty(input.UserName, input.UserEmail, "用户")
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "余额不足提醒"},
			"template": "orange",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**%s**，您的账户余额已低于提醒阈值。", displayName)}},
			map[string]any{"tag": "div", "fields": []any{
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**当前余额**\n$%.2f", input.Balance)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**提醒阈值**\n$%.2f", input.Threshold)}},
			}},
			s.feishuPanelActionElement(ctx, "打开面板", input.RechargeURL),
		},
	}
	return s.queueNotificationCard(ctx, input.UserID, "balance", fmt.Sprintf("balance:%d:%s:%.4f", input.UserID, time.Now().UTC().Format("2006010215"), input.Threshold), card)
}

func (s *FeishuNotificationService) SendSubscriptionExpiryReminder(ctx context.Context, input FeishuSubscriptionExpiryNotification) error {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "订阅到期提醒"},
			"template": "yellow",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("您的订阅 **%s** 将在 **%d 天后** 到期。", input.GroupName, input.DaysRemaining)}},
			map[string]any{"tag": "div", "fields": []any{
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": "**订阅**\n" + input.GroupName}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": "**到期时间**\n" + input.ExpiresAt.Format("2006-01-02 15:04")}},
			}},
			s.feishuPanelActionElement(ctx, "查看面板", ""),
		},
	}
	return s.queueNotificationCard(ctx, input.UserID, "subscription", fmt.Sprintf("subscription-expiry:%d:%d:%s:%d:%s", input.UserID, input.SubscriptionID, input.SourceReminderKey, input.DaysRemaining, input.ExpiresAt.UTC().Format(time.RFC3339)), card)
}

func (s *FeishuNotificationService) SendContentModerationViolation(ctx context.Context, input FeishuContentModerationViolationNotification) error {
	displayName := firstNonEmpty(input.UserName, input.UserEmail, "用户")
	groupName := firstNonEmpty(input.GroupName, "-")
	category := firstNonEmpty(input.Category, "-")
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "账户风控提醒"},
			"template": "orange",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**%s**，您的 API 请求触发了内容风控规则。", displayName)}},
			map[string]any{"tag": "div", "fields": []any{
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**命中分组**\n%s", groupName)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**风险类别**\n%s", category)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**最高分数**\n%.3f", input.Score)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**触发次数**\n%d / %d", input.ViolationCount, input.BanThreshold)}},
			}},
			s.feishuPanelActionElement(ctx, "查看账户", ""),
		},
	}
	businessKey := fmt.Sprintf("moderation-violation:%d:%s:%d", input.UserID, input.Category, input.ViolationCount)
	if input.SourceEventID > 0 {
		businessKey = fmt.Sprintf("moderation-violation:event:%d", input.SourceEventID)
	}
	return s.queueNotificationCard(ctx, input.UserID, "security", businessKey, card)
}

func (s *FeishuNotificationService) SendContentModerationBan(ctx context.Context, input FeishuContentModerationBanNotification) error {
	displayName := firstNonEmpty(input.UserName, input.UserEmail, "用户")
	groupName := firstNonEmpty(input.GroupName, "-")
	category := firstNonEmpty(input.Category, "-")
	banDuration := "-"
	if input.BanDurationMin > 0 {
		banDuration = fmt.Sprintf("%d 分钟", input.BanDurationMin)
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "账户风控封禁通知"},
			"template": "red",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**%s**，您的账户已因触发内容风控规则被自动封禁。", displayName)}},
			map[string]any{"tag": "div", "fields": []any{
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**命中分组**\n%s", groupName)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**风险类别**\n%s", category)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**最高分数**\n%.3f", input.Score)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**触发次数**\n%d / %d", input.ViolationCount, input.BanThreshold)}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": fmt.Sprintf("**封禁时长**\n%s", banDuration)}},
			}},
			s.feishuPanelActionElement(ctx, "查看账户", ""),
		},
	}
	businessKey := fmt.Sprintf("moderation-ban:%d:%s:%d", input.UserID, input.Category, input.ViolationCount)
	if input.SourceEventID > 0 {
		businessKey = fmt.Sprintf("moderation-ban:event:%d", input.SourceEventID)
	}
	return s.queueNotificationCard(ctx, input.UserID, "security", businessKey, card)
}

func (s *FeishuNotificationService) SendTest(ctx context.Context, userID int64) error {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "飞书通知链路测试"},
			"template": "blue",
		},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": "这是一条管理员发起的飞书通知链路测试消息。"}},
			map[string]any{"tag": "div", "fields": []any{
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": "**类型**\n测试通知"}},
				map[string]any{"is_short": true, "text": map[string]any{"tag": "lark_md", "content": "**时间**\n" + time.Now().Format("2006-01-02 15:04:05")}},
			}},
			s.feishuPanelActionElement(ctx, "打开面板", ""),
		},
	}
	return s.sendInteractiveCard(ctx, userID, card)
}

func (s *FeishuNotificationService) queueNotificationCard(ctx context.Context, userID int64, category, businessKey string, card map[string]any) error {
	if s == nil || s.outboxRepo == nil {
		return s.sendInteractiveCard(ctx, userID, card)
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		return ErrFeishuNotificationDisabled
	}
	if s.bindingRepo == nil {
		return ErrFeishuNotificationNotBound
	}
	binding, err := s.bindingRepo.GetFeishuNotificationBinding(ctx, userID, cfg.AppID)
	if err != nil {
		return err
	}
	if !binding.NotificationEnabled {
		return ErrFeishuNotificationDisabled
	}
	preferences, err := s.GetPreferences(ctx, userID)
	if err != nil {
		return err
	}
	if enabled, exists := preferences[category]; exists && !enabled {
		return ErrFeishuNotificationDisabled
	}
	payload, err := json.Marshal(card)
	if err != nil {
		return err
	}
	businessKey = strings.TrimSpace(businessKey)
	if businessKey == "" {
		return fmt.Errorf("feishu notification business key is required")
	}
	orderingKey := ""
	if category == "channel" {
		orderingKey = fmt.Sprintf("feishu:channel:user:%d", userID)
	}
	_, _, err = s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
		DedupeKey:   fmt.Sprintf("feishu:%s:user:%d:%s", cfg.AppID, userID, businessKey),
		OrderingKey: orderingKey,
		UserID:      userID, AppID: cfg.AppID, Category: category, Payload: payload,
	})
	return err
}

func (s *FeishuNotificationService) sendInteractiveCard(ctx context.Context, userID int64, card map[string]any) error {
	_, err := s.sendInteractiveCardWithID(ctx, userID, card)
	return err
}

func (s *FeishuNotificationService) sendInteractiveCardWithID(ctx context.Context, userID int64, card map[string]any) (string, error) {
	return s.sendInteractiveCardWithPreference(ctx, userID, card, true)
}

func (s *FeishuNotificationService) sendInteractiveCardWithPreference(ctx context.Context, userID int64, card map[string]any, respectPreference bool) (string, error) {
	return s.sendInteractiveCardWithPreferenceAndUUID(ctx, userID, card, respectPreference, "")
}

func (s *FeishuNotificationService) sendInteractiveCardWithPreferenceAndUUID(ctx context.Context, userID int64, card map[string]any, respectPreference bool, messageUUID string) (string, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	if !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		return "", ErrFeishuNotificationDisabled
	}
	if s == nil || s.bindingRepo == nil {
		return "", ErrFeishuNotificationNotBound
	}
	binding, err := s.bindingRepo.GetFeishuNotificationBinding(ctx, userID, cfg.AppID)
	if err != nil {
		return "", err
	}
	if respectPreference && !binding.NotificationEnabled {
		return "", ErrFeishuNotificationDisabled
	}
	return s.sendInteractiveCardToOpenIDWithUUID(ctx, cfg, userID, binding.OpenID, card, messageUUID)
}

func (s *FeishuNotificationService) sendInteractiveCardToOpenIDWithUUID(ctx context.Context, cfg FeishuNotificationConfig, userID int64, openID string, card map[string]any, messageUUID string) (string, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return "", ErrFeishuNotificationNotBound
	}
	token, err := s.fetchTenantAccessToken(ctx, cfg)
	if err != nil {
		return "", err
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	messageURL, err := buildFeishuMessageURL(cfg.MessageURL)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"receive_id": openID,
		"msg_type":   "interactive",
		"content":    string(cardJSON),
	}
	if messageUUID = strings.TrimSpace(messageUUID); messageUUID != "" {
		body["uuid"] = messageUUID
	}
	resp, err := req.C().SetTimeout(30*time.Second).R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+token).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetBody(body).
		Post(messageURL)
	if err != nil {
		return "", fmt.Errorf("send feishu message: %w", err)
	}
	if err := validateFeishuNotifyAPIResponse("send feishu message", resp); err != nil {
		return "", err
	}
	messageID := firstNonEmpty(getFeishuNotifyJSON(resp.String(), "data.message_id"), getFeishuNotifyJSON(resp.String(), "message_id"))
	slog.Info("feishu notification sent", "user_id", userID, "app_id", cfg.AppID, "message_id", messageID)
	return messageID, nil
}

func (s *FeishuNotificationService) feishuPanelActionElement(ctx context.Context, label string, fallbackURL string) map[string]any {
	panelURL := strings.TrimSpace(fallbackURL)
	if cfg, err := s.GetConfig(ctx); err == nil {
		panelURL = firstNonEmpty(cfg.PanelURL, panelURL, defaultFeishuPanelPath)
	}
	if label == "" {
		label = "打开面板"
	}
	return map[string]any{
		"tag": "action",
		"actions": []any{
			map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": label},
				"type": "primary",
				"url":  panelURL,
			},
		},
	}
}

func (s *FeishuNotificationService) fetchTenantAccessToken(ctx context.Context, cfg FeishuNotificationConfig) (string, error) {
	cacheKey := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(cfg.AppID),
		strings.TrimSpace(cfg.AppSecret),
		strings.TrimSpace(cfg.TokenURL),
	}, "\x00"))))
	now := time.Now()
	if token := s.cachedTenantToken(cacheKey, now); token != "" {
		return token, nil
	}

	value, err, _ := s.tokenFlight.Do(cacheKey, func() (any, error) {
		if token := s.cachedTenantToken(cacheKey, time.Now()); token != "" {
			return token, nil
		}
		if s.tokenFetchObserver != nil {
			s.tokenFetchObserver()
		}
		resp, err := req.C().SetTimeout(30*time.Second).R().
			SetContext(ctx).
			SetHeader("Accept", "application/json").
			SetHeader("Content-Type", "application/json").
			SetBody(map[string]string{
				"app_id":     strings.TrimSpace(cfg.AppID),
				"app_secret": strings.TrimSpace(cfg.AppSecret),
			}).
			Post(strings.TrimSpace(cfg.TokenURL))
		if err != nil {
			return "", fmt.Errorf("request feishu tenant token: %w", err)
		}
		body := resp.String()
		if err := validateFeishuNotifyAPIResponse("feishu tenant token", resp); err != nil {
			return "", err
		}
		token := firstNonEmpty(getFeishuNotifyJSON(body, "tenant_access_token"), getFeishuNotifyJSON(body, "data.tenant_access_token"))
		if token == "" {
			return "", fmt.Errorf("feishu tenant token response missing tenant_access_token")
		}
		expiresIn := gjson.Get(body, "expire").Int()
		if expiresIn <= 0 {
			expiresIn = gjson.Get(body, "data.expire").Int()
		}
		if expiresIn <= 0 {
			expiresIn = 7200
		}
		cacheSeconds := expiresIn - 300
		if cacheSeconds < 60 {
			cacheSeconds = 60
		}
		s.tokenMu.Lock()
		s.tokenCacheKey = cacheKey
		s.tokenValue = token
		s.tokenExpiresAt = time.Now().Add(time.Duration(cacheSeconds) * time.Second)
		s.tokenMu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	token, _ := value.(string)
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("feishu tenant token is empty")
	}
	return token, nil
}

func (s *FeishuNotificationService) cachedTenantToken(cacheKey string, now time.Time) string {
	if s == nil {
		return ""
	}
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	if s.tokenCacheKey != cacheKey || s.tokenValue == "" || !now.Before(s.tokenExpiresAt) {
		return ""
	}
	return s.tokenValue
}

func buildFeishuMessageURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("receive_id_type") == "" {
		q.Set("receive_id_type", "open_id")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func feishuNotifyAPIErrorCode(body string) int64 {
	body = strings.TrimSpace(body)
	if body == "" {
		return -1
	}
	code := gjson.Get(body, "code")
	if !code.Exists() {
		return -1
	}
	return code.Int()
}

type feishuNotifyAPIError struct {
	Operation string
	Status    int
	Code      string
	Message   string
}

func (e *feishuNotifyAPIError) Error() string {
	if e == nil {
		return "feishu API request failed"
	}
	return fmt.Sprintf("%s status=%d code=%s msg=%s", e.Operation, e.Status, e.Code, e.Message)
}

func newFeishuNotifyAPIError(operation string, resp *req.Response, body string) error {
	return &feishuNotifyAPIError{
		Operation: operation,
		Status:    resp.StatusCode,
		Code:      getFeishuNotifyJSON(body, "code"),
		Message:   firstNonEmpty(getFeishuNotifyJSON(body, "msg"), getFeishuNotifyJSON(body, "message")),
	}
}

func validateFeishuNotifyAPIResponse(operation string, resp *req.Response) error {
	if resp == nil {
		return fmt.Errorf("%s response is nil", operation)
	}
	body := strings.TrimSpace(resp.String())
	if !resp.IsSuccessState() {
		return newFeishuNotifyAPIError(operation, resp, body)
	}
	if body == "" {
		return fmt.Errorf("%s status=%d empty response body", operation, resp.StatusCode)
	}
	if !gjson.Valid(body) {
		return fmt.Errorf("%s status=%d invalid json response", operation, resp.StatusCode)
	}
	if !gjson.Get(body, "code").Exists() {
		return fmt.Errorf("%s status=%d missing code in response", operation, resp.StatusCode)
	}
	if code := feishuNotifyAPIErrorCode(body); code != 0 {
		return newFeishuNotifyAPIError(operation, resp, body)
	}
	return nil
}

func getFeishuNotifyJSON(body string, path string) string {
	value := gjson.Get(body, path)
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.Number {
		return strconv.FormatInt(value.Int(), 10)
	}
	return strings.TrimSpace(value.String())
}
