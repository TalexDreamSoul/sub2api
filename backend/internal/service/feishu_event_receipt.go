package service

import (
	"context"
	"encoding/json"
	"time"
)

type FeishuEventReceiptInput struct {
	AppID         string
	EventID       string
	EventType     string
	TenantKey     string
	SenderOpenID  string
	Payload       json.RawMessage
	PayloadSHA256 string
}

type FeishuEventReceipt struct {
	ID           int64
	AppID        string
	EventID      string
	EventType    string
	TenantKey    string
	SenderOpenID string
	Payload      json.RawMessage
	Attempts     int
}

type FeishuEventReceiptRepository interface {
	Receive(ctx context.Context, input FeishuEventReceiptInput) (int64, bool, error)
	Claim(ctx context.Context, workerID string, limit int, lease time.Duration) ([]FeishuEventReceipt, error)
	Complete(ctx context.Context, id int64, workerID, status string) error
	Retry(ctx context.Context, id int64, workerID string, availableAt time.Time, lastError string) error
}
