package service

import (
	"context"
	"fmt"
	"strings"
)

type FeishuBoundUser struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type feishuBoundUserLister interface {
	ListFeishuBoundUsers(ctx context.Context, appID, search string, limit int) ([]FeishuBoundUser, error)
}

func (s *FeishuNotificationService) ListBoundUsers(ctx context.Context, search string, limit int) ([]FeishuBoundUser, error) {
	if s == nil || s.bindingRepo == nil {
		return nil, fmt.Errorf("feishu binding repository is unavailable")
	}
	lister, ok := s.bindingRepo.(feishuBoundUserLister)
	if !ok {
		return nil, fmt.Errorf("feishu bound user search is unavailable")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return []FeishuBoundUser{}, nil
	}
	return lister.ListFeishuBoundUsers(ctx, cfg.AppID, search, limit)
}
