package service

import "context"

var FeishuNotificationCategories = []string{"balance", "subscription", "quota", "security", "channel"}

type NotificationPreferenceRepository interface {
	Get(ctx context.Context, userID int64, channel string, categories []string) (map[string]bool, error)
	Set(ctx context.Context, userID int64, channel string, preferences map[string]bool) error
}
