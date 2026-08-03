package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const FeishuNotificationCategoryDailyDigest = "daily_digest"

type FeishuDailyUsageStat struct {
	UserID          int64
	ActualCost      float64
	Requests        int64
	Tokens          int64
	Rank            int64
	ActiveUsers     int64
	TotalActualCost float64
}

type feishuDailyDigestUsageReader interface {
	GetFeishuDailyDigestStats(ctx context.Context, userIDs []int64, startTime, endTime time.Time, excludedAPIKeyID int64) (map[int64]FeishuDailyUsageStat, error)
}

type feishuDailyDigestRecipientLister interface {
	ListFeishuDailyDigestUserIDs(ctx context.Context, appID string, afterUserID int64, limit int) ([]int64, error)
}

func (s *FeishuNotificationService) runDailyDigestWorker(ctx context.Context) {
	defer s.workerWG.Done()
	s.enqueueDueDailyDigest(ctx, time.Now())
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.enqueueDueDailyDigest(ctx, now)
		}
	}
}

func (s *FeishuNotificationService) enqueueDueDailyDigest(ctx context.Context, now time.Time) {
	if s == nil || s.dailyUsageRepo == nil || s.outboxRepo == nil {
		return
	}
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil || !cfg.DailyDigestEnabled {
		return
	}
	localNow := now.In(timezone.Location())
	parts := strings.Split(cfg.DailyDigestTime, ":")
	if len(parts) != 2 {
		return
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || localNow.Hour() < hour || (localNow.Hour() == hour && localNow.Minute() < minute) {
		return
	}
	runDate := localNow.Format("2006-01-02")
	s.dailyDigestMu.Lock()
	if s.dailyDigestDate == runDate {
		s.dailyDigestMu.Unlock()
		return
	}
	s.dailyDigestMu.Unlock()
	if err := s.enqueueDailyDigestForDate(ctx, localNow.AddDate(0, 0, -1)); err != nil {
		if ctx.Err() == nil {
			slog.Warn("feishu daily digest enqueue failed", "date", runDate, "error", err)
		}
		return
	}
	s.dailyDigestMu.Lock()
	s.dailyDigestDate = runDate
	s.dailyDigestMu.Unlock()
}

func (s *FeishuNotificationService) enqueueDailyDigestForDate(ctx context.Context, day time.Time) error {
	lister, ok := s.bindingRepo.(feishuDailyDigestRecipientLister)
	if !ok {
		return fmt.Errorf("feishu daily digest recipient listing is unavailable")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	assistantCfg, err := s.GetAssistantConfig(ctx)
	if err != nil {
		return err
	}
	startTime := timezone.StartOfDay(day)
	endTime := startTime.AddDate(0, 0, 1)
	afterUserID := int64(0)
	for {
		userIDs, err := lister.ListFeishuDailyDigestUserIDs(ctx, cfg.AppID, afterUserID, 500)
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return nil
		}
		stats, err := s.dailyUsageRepo.GetFeishuDailyDigestStats(ctx, userIDs, startTime, endTime, assistantCfg.APIKeyID)
		if err != nil {
			return err
		}
		for _, userID := range userIDs {
			stat := stats[userID]
			card := s.renderFeishuDailyDigestCard(ctx, startTime, stat)
			payload, err := marshalFeishuCard(card)
			if err != nil {
				return err
			}
			_, _, err = s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
				DedupeKey:   fmt.Sprintf("feishu:daily-digest:%s:%d", startTime.Format("2006-01-02"), userID),
				OrderingKey: fmt.Sprintf("feishu:user:%d", userID),
				UserID:      userID, AppID: cfg.AppID,
				Category: FeishuNotificationCategoryDailyDigest, Payload: payload,
			})
			if err != nil {
				return err
			}
		}
		afterUserID = userIDs[len(userIDs)-1]
		if len(userIDs) < 500 {
			return nil
		}
	}
}

func (s *FeishuNotificationService) renderFeishuDailyDigestCard(ctx context.Context, day time.Time, stat FeishuDailyUsageStat) map[string]any {
	share := 0.0
	if stat.TotalActualCost > 0 {
		share = stat.ActualCost / stat.TotalActualCost * 100
	}
	rankText := "今日暂无消费排名"
	if stat.Rank > 0 && stat.ActiveUsers > 0 {
		percentile := float64(stat.Rank) / float64(stat.ActiveUsers) * 100
		rankText = fmt.Sprintf("全站排名：第 %d / %d · 前 %.1f%%\n全站消费占比：%.2f%%", stat.Rank, stat.ActiveUsers, percentile, share)
	}
	quotaText, err := s.renderFeishuSubscriptions(ctx, stat.UserID, true)
	if err != nil {
		quotaText = "额度信息暂不可用，请稍后在账户面板查看。"
	}
	content := fmt.Sprintf("实际消费：$%.2f\n请求数：%d\nToken：%d\n\n%s\n\n%s", stat.ActualCost, stat.Requests, stat.Tokens, rankText, quotaText)
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": day.Format("2006-01-02") + " 使用日报"}, "template": "blue"},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": content}},
			s.feishuPanelActionElement(ctx, "查看详细用量", ""),
		},
	}
}

func (s *FeishuNotificationService) renderFeishuDailyUsage(ctx context.Context, userID int64) (string, error) {
	if s == nil || s.dailyUsageRepo == nil {
		return "", fmt.Errorf("daily usage repository is unavailable")
	}
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil {
		return "", err
	}
	now := timezone.Now()
	start := timezone.StartOfDay(now)
	stats, err := s.dailyUsageRepo.GetFeishuDailyDigestStats(ctx, []int64{userID}, start, now, cfg.APIKeyID)
	if err != nil {
		return "", err
	}
	stat := stats[userID]
	share := 0.0
	if stat.TotalActualCost > 0 {
		share = stat.ActualCost / stat.TotalActualCost * 100
	}
	rank := "暂无排名"
	if stat.Rank > 0 {
		rank = fmt.Sprintf("第 %d / %d", stat.Rank, stat.ActiveUsers)
	}
	return fmt.Sprintf("今日使用\n实际消费：$%.2f\n请求数：%d\nToken：%d\n全站排名：%s\n消费占比：%.2f%%", stat.ActualCost, stat.Requests, stat.Tokens, rank, share), nil
}
