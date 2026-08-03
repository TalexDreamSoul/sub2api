package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	FeishuChatKindUnconfigured  = "unconfigured"
	FeishuChatKindUser          = "user"
	FeishuChatKindOperations    = "operations"
	FeishuChatKindManagement    = "management"
	FeishuChatKindNotifications = "notifications"

	FeishuChatStatusPending  = "pending"
	FeishuChatStatusActive   = "active"
	FeishuChatStatusDisabled = "disabled"
)

var (
	ErrFeishuAssistantAdminRequired = infraerrors.Forbidden("FEISHU_ASSISTANT_ADMIN_REQUIRED", "feishu assistant administrator permission is required")
	ErrFeishuChatNotConfigured      = infraerrors.NotFound("FEISHU_CHAT_NOT_CONFIGURED", "feishu chat is not configured")
)

type FeishuAssistantAdmin struct {
	UserID         int64     `json:"user_id"`
	Email          string    `json:"email"`
	Username       string    `json:"username"`
	ConfiguredByID *int64    `json:"configured_by_user_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type FeishuChatBinding struct {
	ID                           int64      `json:"id"`
	AppID                        string     `json:"app_id"`
	TenantKey                    string     `json:"tenant_key"`
	ChatID                       string     `json:"chat_id"`
	ChatName                     string     `json:"chat_name"`
	Kind                         string     `json:"kind"`
	Sub2APIGroupID               *int64     `json:"sub2api_group_id,omitempty"`
	Sub2APIGroupName             string     `json:"sub2api_group_name,omitempty"`
	Status                       string     `json:"status"`
	IncidentNotificationsEnabled bool       `json:"incident_notifications_enabled"`
	DailyDigestEnabled           bool       `json:"daily_digest_enabled"`
	ConfiguredByUserID           *int64     `json:"configured_by_user_id,omitempty"`
	CreatedAt                    time.Time  `json:"created_at"`
	UpdatedAt                    time.Time  `json:"updated_at"`
	DisabledAt                   *time.Time `json:"disabled_at,omitempty"`
}

type ConfigureFeishuChatInput struct {
	ID                           int64
	AppID                        string
	TenantKey                    string
	ChatID                       string
	ChatName                     string
	Kind                         string
	Sub2APIGroupID               int64
	IncidentNotificationsEnabled bool
	DailyDigestEnabled           bool
	ConfiguredByUserID           int64
}

type FeishuChatBindingRepository interface {
	ListAdmins(ctx context.Context) ([]FeishuAssistantAdmin, error)
	AddAdmin(ctx context.Context, userID, configuredByUserID int64) error
	RemoveAdmin(ctx context.Context, userID int64) error
	IsAdmin(ctx context.Context, userID int64) (bool, error)

	ListChats(ctx context.Context) ([]FeishuChatBinding, error)
	GetChatByID(ctx context.Context, id int64) (*FeishuChatBinding, error)
	GetChat(ctx context.Context, appID, tenantKey, chatID string) (*FeishuChatBinding, error)
	UpsertPendingChat(ctx context.Context, appID, tenantKey, chatID, chatName string) (*FeishuChatBinding, error)
	ConfigureChat(ctx context.Context, input ConfigureFeishuChatInput) (*FeishuChatBinding, error)
	DisableChat(ctx context.Context, appID, tenantKey, chatID string) error
	ListActiveChats(ctx context.Context, kinds []string) ([]FeishuChatBinding, error)
}

func (s *FeishuNotificationService) ListFeishuAssistantAdmins(ctx context.Context) ([]FeishuAssistantAdmin, error) {
	if s == nil || s.chatRepo == nil {
		return nil, fmt.Errorf("feishu chat repository is unavailable")
	}
	return s.chatRepo.ListAdmins(ctx)
}

func (s *FeishuNotificationService) AddFeishuAssistantAdmin(ctx context.Context, actorID, userID int64) error {
	if s == nil || s.chatRepo == nil || s.userRepo == nil || s.bindingRepo == nil {
		return fmt.Errorf("feishu assistant administrator service is unavailable")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil || !user.IsAdmin() || !user.IsActive() {
		return infraerrors.BadRequest("FEISHU_ASSISTANT_ADMIN_INVALID", "only active Sub2API administrators can be assigned")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	if _, err := s.bindingRepo.GetFeishuNotificationBinding(ctx, userID, cfg.AppID); err != nil {
		return infraerrors.BadRequest("FEISHU_ASSISTANT_ADMIN_NOT_BOUND", "administrator must bind Feishu before assignment")
	}
	return s.chatRepo.AddAdmin(ctx, userID, actorID)
}

func (s *FeishuNotificationService) RemoveFeishuAssistantAdmin(ctx context.Context, userID int64) error {
	if s == nil || s.chatRepo == nil {
		return fmt.Errorf("feishu chat repository is unavailable")
	}
	admins, err := s.chatRepo.ListAdmins(ctx)
	if err != nil {
		return err
	}
	if len(admins) <= 1 && len(admins) == 1 && admins[0].UserID == userID {
		return infraerrors.Conflict("FEISHU_ASSISTANT_LAST_ADMIN", "at least one feishu assistant administrator is required")
	}
	return s.chatRepo.RemoveAdmin(ctx, userID)
}

func (s *FeishuNotificationService) IsFeishuAssistantAdmin(ctx context.Context, userID int64) (bool, error) {
	if s == nil || s.chatRepo == nil || s.userRepo == nil || userID <= 0 {
		return false, nil
	}
	allowed, err := s.chatRepo.IsAdmin(ctx, userID)
	if err != nil || !allowed {
		return false, err
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return user != nil && user.IsAdmin() && user.IsActive(), nil
}

func (s *FeishuNotificationService) ListFeishuChatBindings(ctx context.Context) ([]FeishuChatBinding, error) {
	if s == nil || s.chatRepo == nil {
		return nil, fmt.Errorf("feishu chat repository is unavailable")
	}
	return s.chatRepo.ListChats(ctx)
}

func (s *FeishuNotificationService) UpdateFeishuChatBinding(ctx context.Context, actorID int64, input ConfigureFeishuChatInput) (*FeishuChatBinding, error) {
	if s == nil || s.chatRepo == nil {
		return nil, fmt.Errorf("feishu chat repository is unavailable")
	}
	current, err := s.chatRepo.GetChatByID(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	input.AppID = current.AppID
	input.TenantKey = current.TenantKey
	input.ChatID = current.ChatID
	if strings.TrimSpace(input.ChatName) == "" {
		input.ChatName = current.ChatName
	}
	input.ConfiguredByUserID = actorID
	if err := s.validateFeishuChatConfiguration(ctx, &input); err != nil {
		return nil, err
	}
	return s.chatRepo.ConfigureChat(ctx, input)
}

func (s *FeishuNotificationService) validateFeishuChatConfiguration(ctx context.Context, input *ConfigureFeishuChatInput) error {
	if input == nil {
		return infraerrors.BadRequest("FEISHU_CHAT_INVALID", "chat configuration is required")
	}
	input.Kind = normalizeFeishuChatKind(input.Kind)
	switch input.Kind {
	case FeishuChatKindUser, FeishuChatKindOperations:
		if input.Sub2APIGroupID <= 0 {
			return infraerrors.BadRequest("FEISHU_CHAT_GROUP_REQUIRED", "Sub2API group is required for this chat kind")
		}
		if s.groupRepo == nil {
			return fmt.Errorf("group repository is unavailable")
		}
		group, err := s.groupRepo.GetByIDLite(ctx, input.Sub2APIGroupID)
		if err != nil {
			return err
		}
		if group == nil {
			return ErrGroupNotFound
		}
	case FeishuChatKindManagement, FeishuChatKindNotifications:
		input.Sub2APIGroupID = 0
	default:
		return infraerrors.BadRequest("FEISHU_CHAT_KIND_INVALID", "invalid feishu chat kind")
	}
	switch input.Kind {
	case FeishuChatKindUser:
		input.IncidentNotificationsEnabled = false
	case FeishuChatKindManagement, FeishuChatKindNotifications:
		input.DailyDigestEnabled = false
	}
	return nil
}

func normalizeFeishuChatKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "user", "usage", "使用群", "用户群":
		return FeishuChatKindUser
	case "operations", "operation", "ops", "维护群", "运维群":
		return FeishuChatKindOperations
	case "management", "admin", "管理群":
		return FeishuChatKindManagement
	case "notifications", "notification", "alerts", "通知群", "告警群":
		return FeishuChatKindNotifications
	default:
		return ""
	}
}

func feishuChatKindLabel(kind string) string {
	switch kind {
	case FeishuChatKindUser:
		return "用户使用群"
	case FeishuChatKindOperations:
		return "维护群"
	case FeishuChatKindManagement:
		return "管理群"
	case FeishuChatKindNotifications:
		return "通知群"
	default:
		return "未配置群"
	}
}

func (s *FeishuNotificationService) renderFeishuGroupUsage(ctx context.Context, groupID int64, start, end time.Time) (string, error) {
	if s == nil || s.usageLogRepo == nil || s.groupRepo == nil {
		return "群用量暂不可用，请稍后再试。", nil
	}
	group, err := s.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil {
		return "", err
	}
	stats, err := s.usageLogRepo.GetGroupStatsWithFilters(ctx, start, end, 0, 0, 0, groupID, nil, nil, nil)
	if err != nil {
		return "", err
	}
	if len(stats) == 0 {
		return fmt.Sprintf("%s\n时段：%s 至 %s\n暂无使用记录。", group.Name, start.Format("01-02 15:04"), end.Format("01-02 15:04")), nil
	}
	stat := stats[0]
	return fmt.Sprintf("%s 用量\n时段：%s 至 %s\n请求数：%d\nToken：%d\n实际消费：$%.2f", group.Name, start.Format("01-02 15:04"), end.Format("01-02 15:04"), stat.Requests, stat.TotalTokens, stat.ActualCost), nil
}

func (s *FeishuNotificationService) renderFeishuGroupToday(ctx context.Context, groupID int64) (string, error) {
	now := time.Now().In(timezone.Location())
	return s.renderFeishuGroupUsage(ctx, groupID, timezone.StartOfDay(now), now)
}

func (s *FeishuNotificationService) renderFeishuGroupStatus(ctx context.Context, groupID int64) (string, error) {
	if s == nil || s.groupRepo == nil {
		return "群状态暂不可用，请稍后再试。", nil
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n平台：%s\n状态：%s\n账号：%d 个，正常 %d 个，限流 %d 个", group.Name, group.Platform, group.Status, group.AccountCount, group.ActiveAccountCount, group.RateLimitedAccountCount), nil
}

func (s *FeishuNotificationService) renderFeishuSystemStatus(ctx context.Context) (string, error) {
	if s == nil || s.usageLogRepo == nil {
		return "系统状态暂不可用，请稍后再试。", nil
	}
	stats, err := s.usageLogRepo.GetDashboardStats(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Sub2API 系统状态\n今日请求：%d\n今日活跃用户：%d\n今日 Token：%d\n正常账号：%d / %d\n异常：%d，限流：%d，过载：%d", stats.TodayRequests, stats.ActiveUsers, stats.TodayTokens, stats.NormalAccounts, stats.TotalAccounts, stats.ErrorAccounts, stats.RateLimitAccounts, stats.OverloadAccounts), nil
}

func isFeishuChatNotConfigured(err error) bool {
	return errors.Is(err, ErrFeishuChatNotConfigured)
}
