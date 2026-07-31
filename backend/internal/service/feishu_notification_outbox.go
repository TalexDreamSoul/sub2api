package service

import (
	"context"
	"encoding/json"
	"time"
)

const (
	FeishuNotificationCategoryTest         = "test"
	FeishuNotificationCategoryAdminService = "admin_service"
	FeishuNotificationCategoryBotReply     = "bot_reply"
)

type FeishuAdminMessageInput struct {
	UserID         int64
	Content        string
	IdempotencyKey string
	CreatedBy      *int64
}

type FeishuNotificationOutboxInput struct {
	DedupeKey       string
	OrderingKey     string
	UserID          int64
	RecipientOpenID string
	AppID           string
	Category        string
	Payload         json.RawMessage
	CreatedBy       *int64
}

type FeishuNotificationOutboxItem struct {
	ID              int64
	UserID          int64
	RecipientOpenID string
	AppID           string
	Category        string
	Payload         json.RawMessage
	Attempts        int
	CreatedAt       time.Time
}

type FeishuNotificationDelivery struct {
	ID                int64      `json:"id"`
	UserID            int64      `json:"user_id,omitempty"`
	Category          string     `json:"category"`
	Status            string     `json:"status"`
	Attempts          int        `json:"attempts"`
	ProviderMessageID string     `json:"provider_message_id,omitempty"`
	ErrorCode         string     `json:"error_code,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
}

type FeishuNotificationOutboxStats struct {
	Pending         int64      `json:"pending"`
	Processing      int64      `json:"processing"`
	Dead            int64      `json:"dead"`
	OldestCreatedAt *time.Time `json:"oldest_created_at,omitempty"`
	LastError       string     `json:"-"`
}

type FeishuNotificationOutboxRepository interface {
	Enqueue(ctx context.Context, input FeishuNotificationOutboxInput) (int64, bool, error)
	Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]FeishuNotificationOutboxItem, error)
	MarkSent(ctx context.Context, id int64, workerID, providerMessageID string) error
	Retry(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error
	MarkDead(ctx context.Context, id int64, workerID, lastError string) error
	ListRecent(ctx context.Context, limit int) ([]FeishuNotificationDelivery, error)
	Stats(ctx context.Context) (FeishuNotificationOutboxStats, error)
}
