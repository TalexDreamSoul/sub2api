package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const SettingKeyQuotaStatusConfig = "quota_status_config"

type QuotaStatusAccountConfig struct {
	AccountID   int64  `json:"account_id"`
	DisplayName string `json:"display_name"`
	ShowName    bool   `json:"show_name"`
}

type QuotaStatusGroupConfig struct {
	ID          string                     `json:"id"`
	GroupID     int64                      `json:"group_id"`
	DisplayName string                     `json:"display_name"`
	Accounts    []QuotaStatusAccountConfig `json:"accounts"`
}

type QuotaStatusDisplayConfig struct {
	ShowRateMultiplier    bool `json:"show_rate_multiplier"`
	ShowModelDistribution bool `json:"show_model_distribution"`
	ShowDailyCurve        bool `json:"show_daily_curve"`
	ShowSchedulingQuota   bool `json:"show_scheduling_quota"`
	CurveDays             int  `json:"curve_days"`
}

type QuotaStatusConfig struct {
	Enabled     bool                     `json:"enabled"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Display     QuotaStatusDisplayConfig `json:"display"`
	Groups      []QuotaStatusGroupConfig `json:"groups"`
}

type QuotaStatusDimension struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Used        *float64   `json:"used,omitempty"`
	Limit       *float64   `json:"limit,omitempty"`
	Utilization *float64   `json:"utilization,omitempty"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
	Unit        string     `json:"unit"`
}

// QuotaStatusCurvePoint 表示单个时间点的调度/用量数据
type QuotaStatusCurvePoint struct {
	Date     string  `json:"date"`
	Label    string  `json:"label"`
	Cost     float64 `json:"cost"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
}

// QuotaStatusModelStat 模型分布统计
type QuotaStatusModelStat struct {
	Model      string  `json:"model"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	ActualCost float64 `json:"actual_cost"`
}

type QuotaStatusAccount struct {
	Name              string                  `json:"name"`
	Platform          string                  `json:"platform"`
	Type              string                  `json:"type"`
	Status            string                  `json:"status"`
	RateMultiplier    float64                 `json:"rate_multiplier"`
	Schedulable       bool                    `json:"schedulable"`
	Priority          int                     `json:"priority"`
	Dimensions        []QuotaStatusDimension  `json:"dimensions"`
	DailyCurve        []QuotaStatusCurvePoint `json:"daily_curve,omitempty"`
	ModelDistribution []QuotaStatusModelStat  `json:"model_distribution,omitempty"`
}

type QuotaStatusGroup struct {
	Name     string               `json:"name"`
	Platform string               `json:"platform"`
	Accounts []QuotaStatusAccount `json:"accounts"`
}

type QuotaStatusSnapshot struct {
	Enabled     bool                     `json:"enabled"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Display     QuotaStatusDisplayConfig `json:"display"`
	UpdatedAt   time.Time                `json:"updated_at"`
	Groups      []QuotaStatusGroup       `json:"groups"`
}

type QuotaStatusService struct {
	adminService        AdminService
	settingService      *SettingService
	accountUsageService *AccountUsageService
}

func NewQuotaStatusService(adminService AdminService, settingService *SettingService, accountUsageService *AccountUsageService) *QuotaStatusService {
	return &QuotaStatusService{
		adminService:        adminService,
		settingService:      settingService,
		accountUsageService: accountUsageService,
	}
}

func defaultQuotaStatusConfig() QuotaStatusConfig {
	return QuotaStatusConfig{
		Title:       "账号额度状态",
		Description: "查看各渠道账号的额度使用情况。",
		Display: QuotaStatusDisplayConfig{
			ShowRateMultiplier:    true,
			ShowModelDistribution: true,
			ShowDailyCurve:        true,
			ShowSchedulingQuota:   true,
			CurveDays:             7,
		},
		Groups: []QuotaStatusGroupConfig{},
	}
}

func (s *QuotaStatusService) GetConfig(ctx context.Context) (QuotaStatusConfig, error) {
	config := defaultQuotaStatusConfig()
	if s == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return config, nil
	}
	raw, err := s.settingService.settingRepo.GetValue(ctx, SettingKeyQuotaStatusConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return config, nil
		}
		return config, fmt.Errorf("get quota status config: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return config, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return defaultQuotaStatusConfig(), fmt.Errorf("parse quota status config: %w", err)
	}
	normalizeQuotaStatusConfig(&config)
	return config, nil
}

func (s *QuotaStatusService) UpdateConfig(ctx context.Context, config QuotaStatusConfig) (QuotaStatusConfig, error) {
	normalizeQuotaStatusConfig(&config)
	if err := s.validateConfig(ctx, config); err != nil {
		return QuotaStatusConfig{}, err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return QuotaStatusConfig{}, fmt.Errorf("marshal quota status config: %w", err)
	}
	if err := s.settingService.settingRepo.Set(ctx, SettingKeyQuotaStatusConfig, string(payload)); err != nil {
		return QuotaStatusConfig{}, fmt.Errorf("save quota status config: %w", err)
	}
	return config, nil
}

func normalizeQuotaStatusConfig(config *QuotaStatusConfig) {
	config.Title = strings.TrimSpace(config.Title)
	config.Description = strings.TrimSpace(config.Description)
	if config.Title == "" {
		config.Title = defaultQuotaStatusConfig().Title
	}
	if config.Display.CurveDays <= 0 {
		config.Display.CurveDays = 7
	}
	if config.Display.CurveDays > 30 {
		config.Display.CurveDays = 30
	}
	if config.Groups == nil {
		config.Groups = []QuotaStatusGroupConfig{}
	}
	for i := range config.Groups {
		group := &config.Groups[i]
		group.ID = strings.TrimSpace(group.ID)
		if group.ID == "" {
			group.ID = fmt.Sprintf("group-%d", group.GroupID)
		}
		group.DisplayName = strings.TrimSpace(group.DisplayName)
		if group.Accounts == nil {
			group.Accounts = []QuotaStatusAccountConfig{}
		}
		seen := make(map[int64]struct{}, len(group.Accounts))
		accounts := group.Accounts[:0]
		for _, account := range group.Accounts {
			if account.AccountID <= 0 {
				continue
			}
			if _, ok := seen[account.AccountID]; ok {
				continue
			}
			seen[account.AccountID] = struct{}{}
			account.DisplayName = strings.TrimSpace(account.DisplayName)
			accounts = append(accounts, account)
		}
		group.Accounts = accounts
	}
}

func (s *QuotaStatusService) validateConfig(ctx context.Context, config QuotaStatusConfig) error {
	if len(config.Title) > 100 || len(config.Description) > 500 {
		return fmt.Errorf("quota status title or description is too long")
	}
	if len(config.Groups) > 50 {
		return fmt.Errorf("quota status supports at most 50 groups")
	}
	seenGroupIDs := make(map[int64]struct{}, len(config.Groups))
	for _, item := range config.Groups {
		if item.GroupID <= 0 {
			return fmt.Errorf("group_id must be positive")
		}
		if _, ok := seenGroupIDs[item.GroupID]; ok {
			return fmt.Errorf("group_id %d is duplicated", item.GroupID)
		}
		seenGroupIDs[item.GroupID] = struct{}{}
		group, err := s.adminService.GetGroup(ctx, item.GroupID)
		if err != nil {
			return fmt.Errorf("get group %d: %w", item.GroupID, err)
		}
		if len(item.DisplayName) > 100 {
			return fmt.Errorf("display name for group %d is too long", item.GroupID)
		}
		if len(item.Accounts) > 500 {
			return fmt.Errorf("group %d supports at most 500 accounts", item.GroupID)
		}
		ids := make([]int64, 0, len(item.Accounts))
		for _, account := range item.Accounts {
			ids = append(ids, account.AccountID)
			if len(account.DisplayName) > 100 {
				return fmt.Errorf("display name for account %d is too long", account.AccountID)
			}
		}
		accounts, err := s.adminService.GetAccountsByIDs(ctx, ids)
		if err != nil {
			return fmt.Errorf("get accounts for group %d: %w", item.GroupID, err)
		}
		if len(accounts) != len(ids) {
			return fmt.Errorf("one or more accounts in group %d do not exist", item.GroupID)
		}
		for _, account := range accounts {
			if account.Platform != group.Platform || !quotaStatusAccountBelongsToGroup(account, item.GroupID) {
				return fmt.Errorf("account %d does not belong to group %d", account.ID, item.GroupID)
			}
		}
	}
	return nil
}

func quotaStatusAccountBelongsToGroup(account *Account, groupID int64) bool {
	for _, id := range account.GroupIDs {
		if id == groupID {
			return true
		}
	}
	for _, group := range account.AccountGroups {
		if group.GroupID == groupID {
			return true
		}
	}
	return false
}

func (s *QuotaStatusService) GetSnapshot(ctx context.Context) (QuotaStatusSnapshot, error) {
	config, err := s.GetConfig(ctx)
	if err != nil {
		return QuotaStatusSnapshot{}, err
	}
	snapshot := QuotaStatusSnapshot{
		Enabled:     config.Enabled,
		Title:       config.Title,
		Description: config.Description,
		Display:     config.Display,
		UpdatedAt:   time.Now(),
		Groups:      []QuotaStatusGroup{},
	}
	if !config.Enabled {
		return snapshot, nil
	}

	for _, groupConfig := range config.Groups {
		group, err := s.adminService.GetGroup(ctx, groupConfig.GroupID)
		if err != nil {
			continue
		}
		accountIDs := make([]int64, 0, len(groupConfig.Accounts))
		accountConfigByID := make(map[int64]QuotaStatusAccountConfig, len(groupConfig.Accounts))
		for _, item := range groupConfig.Accounts {
			accountIDs = append(accountIDs, item.AccountID)
			accountConfigByID[item.AccountID] = item
		}
		accounts, err := s.adminService.GetAccountsByIDs(ctx, accountIDs)
		if err != nil {
			continue
		}
		accountByID := make(map[int64]*Account, len(accounts))
		for _, account := range accounts {
			accountByID[account.ID] = account
		}

		publicGroup := QuotaStatusGroup{
			Name:     quotaStatusFirstNonEmpty(groupConfig.DisplayName, group.Name),
			Platform: group.Platform,
			Accounts: []QuotaStatusAccount{},
		}
		results := make([]*QuotaStatusAccount, len(groupConfig.Accounts))
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 8)
		curveDays := config.Display.CurveDays
		if curveDays <= 0 {
			curveDays = 7
		}
		for index, item := range groupConfig.Accounts {
			account := accountByID[item.AccountID]
			if account == nil || !quotaStatusAccountBelongsToGroup(account, groupConfig.GroupID) {
				continue
			}
			wg.Add(1)
			go func(index int, account *Account, item QuotaStatusAccountConfig) {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				var usage *UsageInfo
				if s.accountUsageService != nil {
					usage, _ = s.accountUsageService.GetPassiveUsageForAccount(ctx, account)
				}
				name := fmt.Sprintf("账号 %d", index+1)
				if item.ShowName {
					name = quotaStatusFirstNonEmpty(item.DisplayName, account.Name)
				}
				rateMult := 1.0
				if account.RateMultiplier != nil {
					rateMult = *account.RateMultiplier
				}
				result := &QuotaStatusAccount{
					Name:           name,
					Platform:       account.Platform,
					Type:           account.Type,
					Status:         publicAccountStatus(account),
					RateMultiplier: rateMult,
					Schedulable:    account.Schedulable,
					Priority:       account.Priority,
					Dimensions:     buildQuotaStatusDimensions(account, usage),
				}
				// 每日调度曲线
				if config.Display.ShowDailyCurve {
					result.DailyCurve = s.buildDailyCurve(ctx, account, curveDays)
				}
				// 模型分布
				if config.Display.ShowModelDistribution {
					result.ModelDistribution = s.buildModelDistribution(ctx, account, curveDays)
				}
				results[index] = result
			}(index, account, accountConfigByID[account.ID])
		}
		wg.Wait()
		for _, result := range results {
			if result != nil {
				publicGroup.Accounts = append(publicGroup.Accounts, *result)
			}
		}
		snapshot.Groups = append(snapshot.Groups, publicGroup)
	}
	return snapshot, nil
}

func publicAccountStatus(account *Account) string {
	if account == nil || account.Status != StatusActive || !account.Schedulable {
		return "unavailable"
	}
	now := time.Now()
	if account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt) {
		return "unavailable"
	}
	if account.IsAPIKeyOrBedrock() && account.IsQuotaExceeded() {
		return "unavailable"
	}
	if (account.RateLimitResetAt != nil && account.RateLimitResetAt.After(now)) ||
		(account.OverloadUntil != nil && account.OverloadUntil.After(now)) ||
		(account.TempUnschedulableUntil != nil && account.TempUnschedulableUntil.After(now)) {
		return "limited"
	}
	return "available"
}

func buildQuotaStatusDimensions(account *Account, usage *UsageInfo) []QuotaStatusDimension {
	dimensions := make([]QuotaStatusDimension, 0, 12)
	appendMoneyDimension := func(key, label string, used, limit float64, resetsAt time.Time) {
		if limit <= 0 {
			return
		}
		utilization := clampUtilization(used / limit * 100)
		dimension := QuotaStatusDimension{Key: key, Label: label, Used: floatPtr(used), Limit: floatPtr(limit), Utilization: floatPtr(utilization), Unit: "USD"}
		if !resetsAt.IsZero() {
			dimension.ResetsAt = &resetsAt
		}
		dimensions = append(dimensions, dimension)
	}
	appendMoneyDimension("total", "总额度", account.GetQuotaUsed(), account.GetQuotaLimit(), time.Time{})
	appendMoneyDimension("daily", "日额度", account.GetQuotaDailyUsed(), account.GetQuotaDailyLimit(), account.getExtraTime("quota_daily_reset_at"))
	appendMoneyDimension("weekly", "周额度", account.GetQuotaWeeklyUsed(), account.GetQuotaWeeklyLimit(), account.getExtraTime("quota_weekly_reset_at"))

	appendProgress := func(key, label string, progress *UsageProgress) {
		if progress == nil {
			return
		}
		utilization := clampUtilization(progress.Utilization)
		dimensions = append(dimensions, QuotaStatusDimension{Key: key, Label: label, Utilization: floatPtr(utilization), ResetsAt: progress.ResetsAt, Unit: "percent"})
	}
	if usage != nil {
		appendProgress("five_hour", "5 小时", usage.FiveHour)
		appendProgress("seven_day", "7 天", usage.SevenDay)
		appendProgress("seven_day_sonnet", "7 天 Sonnet", usage.SevenDaySonnet)
		appendProgress("seven_day_fable", "7 天 Fable", usage.SevenDayFable)
		appendProgress("gemini_shared_daily", "共享日额度", usage.GeminiSharedDaily)
		appendProgress("gemini_pro_daily", "Pro 日额度", usage.GeminiProDaily)
		appendProgress("gemini_flash_daily", "Flash 日额度", usage.GeminiFlashDaily)
		appendProgress("gemini_shared_minute", "共享分钟额度", usage.GeminiSharedMinute)
		appendProgress("gemini_pro_minute", "Pro 分钟额度", usage.GeminiProMinute)
		appendProgress("gemini_flash_minute", "Flash 分钟额度", usage.GeminiFlashMinute)
		models := make([]string, 0, len(usage.AntigravityQuota))
		for model := range usage.AntigravityQuota {
			models = append(models, model)
		}
		sort.Strings(models)
		for _, model := range models {
			quota := usage.AntigravityQuota[model]
			if quota == nil {
				continue
			}
			utilization := clampUtilization(float64(quota.Utilization))
			dimension := QuotaStatusDimension{Key: "antigravity_" + model, Label: model, Utilization: floatPtr(utilization), Unit: "percent"}
			if reset, err := time.Parse(time.RFC3339, quota.ResetTime); err == nil {
				dimension.ResetsAt = &reset
			}
			dimensions = append(dimensions, dimension)
		}
		if usage.GrokRequestQuota != nil && usage.GrokRequestQuota.Limit != nil && usage.GrokRequestQuota.Remaining != nil && *usage.GrokRequestQuota.Limit > 0 {
			used := float64(*usage.GrokRequestQuota.Limit - *usage.GrokRequestQuota.Remaining)
			limit := float64(*usage.GrokRequestQuota.Limit)
			dimensions = append(dimensions, QuotaStatusDimension{Key: "grok_requests", Label: "请求额度", Used: &used, Limit: &limit, Utilization: floatPtr(clampUtilization(used / limit * 100)), Unit: "requests"})
		}
		if usage.GrokTokenQuota != nil && usage.GrokTokenQuota.Limit != nil && usage.GrokTokenQuota.Remaining != nil && *usage.GrokTokenQuota.Limit > 0 {
			used := float64(*usage.GrokTokenQuota.Limit - *usage.GrokTokenQuota.Remaining)
			limit := float64(*usage.GrokTokenQuota.Limit)
			dimensions = append(dimensions, QuotaStatusDimension{Key: "grok_tokens", Label: "Token 额度", Used: &used, Limit: &limit, Utilization: floatPtr(clampUtilization(used / limit * 100)), Unit: "tokens"})
		}
	}
	return dimensions
}

// buildDailyCurve 构建账号近 N 天的每日用量曲线
func (s *QuotaStatusService) buildDailyCurve(ctx context.Context, account *Account, days int) []QuotaStatusCurvePoint {
	if s.accountUsageService == nil || days <= 0 {
		return nil
	}
	now := timezone.Now()
	endTime := timezone.StartOfDay(now.AddDate(0, 0, 1))
	startTime := timezone.StartOfDay(now.AddDate(0, 0, -days+1))

	stats, err := s.accountUsageService.GetAccountUsageStats(ctx, account.ID, startTime, endTime)
	if err != nil {
		return nil
	}
	curve := make([]QuotaStatusCurvePoint, 0, len(stats.History))
	for _, h := range stats.History {
		curve = append(curve, QuotaStatusCurvePoint{
			Date:     h.Date,
			Label:    h.Label,
			Cost:     h.ActualCost,
			Requests: h.Requests,
			Tokens:   h.Tokens,
		})
	}
	return curve
}

// buildModelDistribution 构建账号近 N 天的模型分布
func (s *QuotaStatusService) buildModelDistribution(ctx context.Context, account *Account, days int) []QuotaStatusModelStat {
	if s.accountUsageService == nil || days <= 0 {
		return nil
	}
	now := timezone.Now()
	endTime := timezone.StartOfDay(now.AddDate(0, 0, 1))
	startTime := timezone.StartOfDay(now.AddDate(0, 0, -days+1))

	stats, err := s.accountUsageService.GetAccountUsageStats(ctx, account.ID, startTime, endTime)
	if err != nil {
		return nil
	}
	// 按 actual_cost 降序排列，取前 10 个模型
	sort.Slice(stats.Models, func(i, j int) bool {
		return stats.Models[i].ActualCost > stats.Models[j].ActualCost
	})
	limit := len(stats.Models)
	if limit > 10 {
		limit = 10
	}
	result := make([]QuotaStatusModelStat, 0, limit)
	for _, m := range stats.Models[:limit] {
		result = append(result, QuotaStatusModelStat{
			Model:      m.Model,
			Requests:   m.Requests,
			Tokens:     m.TotalTokens,
			ActualCost: m.ActualCost,
		})
	}
	return result
}

func quotaStatusFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func floatPtr(value float64) *float64 {
	return &value
}

func clampUtilization(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return math.Min(value, 100)
}
