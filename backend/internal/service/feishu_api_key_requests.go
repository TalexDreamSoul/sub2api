package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	FeishuAPIKeyRequestModeDisabled = "disabled"
	FeishuAPIKeyRequestModeManual   = "manual"
	FeishuAPIKeyRequestModeAuto     = "auto"

	FeishuAPIKeyRequestStatusPending    = "pending"
	FeishuAPIKeyRequestStatusProcessing = "processing"
	FeishuAPIKeyRequestStatusIssued     = "issued"
	FeishuAPIKeyRequestStatusRejected   = "rejected"
	FeishuAPIKeyRequestStatusCancelled  = "cancelled"
)

var (
	ErrFeishuAPIKeyRequestNotFound = infraerrors.NotFound("FEISHU_API_KEY_REQUEST_NOT_FOUND", "Feishu API key request not found")
	ErrFeishuAPIKeyRequestBusy     = infraerrors.Conflict("FEISHU_API_KEY_REQUEST_BUSY", "An API key request is already pending")
)

type FeishuAPIKeyRequest struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"user_id"`
	RequestedGroupID int64      `json:"requested_group_id"`
	RequestedName    string     `json:"requested_name"`
	SourceEventID    string     `json:"-"`
	Status           string     `json:"status"`
	APIKeyID         *int64     `json:"api_key_id,omitempty"`
	ReviewedBy       *int64     `json:"reviewed_by,omitempty"`
	ReviewNote       string     `json:"review_note,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
}

type CreateFeishuAPIKeyRequestInput struct {
	UserID           int64
	RequestedGroupID int64
	RequestedName    string
	SourceEventID    string
}

type FeishuAPIKeyRequestRepository interface {
	Create(ctx context.Context, input CreateFeishuAPIKeyRequestInput) (*FeishuAPIKeyRequest, bool, error)
	Get(ctx context.Context, id int64) (*FeishuAPIKeyRequest, error)
	List(ctx context.Context, status string, limit int) ([]FeishuAPIKeyRequest, error)
	Claim(ctx context.Context, id int64) (*FeishuAPIKeyRequest, error)
	ResetPending(ctx context.Context, id int64, note string) error
	MarkIssued(ctx context.Context, id, apiKeyID int64, reviewedBy *int64) error
	Reject(ctx context.Context, id int64, reviewedBy int64, note string) (*FeishuAPIKeyRequest, error)
}

func isFeishuAPIKeyRequestCommand(value string) bool {
	switch normalizeFeishuBotCommand(value) {
	case "/申请key", "/申请apikey", "/request-key", "/requestkey":
		return true
	default:
		return false
	}
}

func (s *FeishuNotificationService) buildFeishuAPIKeyRequestCard(ctx context.Context, binding *FeishuUserIdentityBinding, languages ...string) (map[string]any, error) {
	if s == nil || s.apiKeyService == nil || binding == nil || binding.UserID <= 0 {
		return nil, ErrFeishuNotificationNotBound
	}
	language := feishuLanguageChinese
	if len(languages) > 0 {
		language = normalizeFeishuLanguage(languages[0])
	}
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.APIKeyRequestMode == FeishuAPIKeyRequestModeDisabled {
		return simpleFeishuAssistantCard(localizeFeishu("API Key 申请", "API Key request", language), localizeFeishu("管理员暂未开放飞书 API Key 申请。", "The administrator has not enabled Feishu API Key requests.", language), "grey"), nil
	}
	groups, err := s.apiKeyService.GetAvailableGroups(ctx, binding.UserID)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return simpleFeishuAssistantCard(localizeFeishu("API Key 申请", "API Key request", language), localizeFeishu("当前账户没有可申请的分组。", "No eligible groups are available for this account.", language), "orange"), nil
	}
	if cfg.DefaultGroupID > 0 {
		for i := range groups {
			if groups[i].ID == cfg.DefaultGroupID {
				groups[0], groups[i] = groups[i], groups[0]
				break
			}
		}
	}
	actions := make([]any, 0, min(len(groups), 8))
	for i := range groups {
		if i >= 8 {
			break
		}
		group := groups[i]
		actions = append(actions, map[string]any{
			"tag":  "button",
			"type": "primary",
			"text": map[string]any{"tag": "plain_text", "content": group.Name},
			"value": map[string]any{
				"action":   "api_key_request",
				"group_id": group.ID,
				"language": language,
			},
			"confirm": map[string]any{
				"title": map[string]any{"tag": "plain_text", "content": localizeFeishu("确认申请 API Key", "Confirm API Key request", language)},
				"text":  map[string]any{"tag": "plain_text", "content": localizeFeishu("申请分组：", "Group: ", language) + group.Name + localizeFeishu("。API Key 仅在站内安全页面展示。", ". The full API Key is only shown on the secure account page.", language)},
			},
		})
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": localizeFeishu("申请 API Key", "Request an API Key", language)}, "template": "blue"},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": localizeFeishu("请选择要申请的分组。提交后将按管理员策略自动签发或进入人工审批。", "Choose a group. The request will be issued automatically or sent for approval according to administrator policy.", language)}},
			map[string]any{"tag": "action", "actions": actions},
			s.feishuPanelActionElement(ctx, localizeFeishu("打开账户面板", "Open account panel", language), ""),
		},
	}, nil
}

func (s *FeishuNotificationService) handleFeishuAPIKeyRequestAction(ctx context.Context, receipt *FeishuEventReceipt, binding *FeishuUserIdentityBinding, groupID int64, languages ...string) (string, error) {
	if s == nil || s.apiKeyRequestRepo == nil || s.apiKeyService == nil {
		return "", fmt.Errorf("feishu API key request service is unavailable")
	}
	language := feishuLanguageChinese
	if len(languages) > 0 {
		language = normalizeFeishuLanguage(languages[0])
	}
	if binding == nil || binding.UserID <= 0 || groupID <= 0 {
		return s.enqueueFeishuBotReply(ctx, receipt, 0, localizeFeishu("API Key 申请参数无效。", "Invalid API Key request parameters.", language))
	}
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil {
		return "", err
	}
	if cfg.APIKeyRequestMode == FeishuAPIKeyRequestModeDisabled {
		return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, localizeFeishu("管理员暂未开放飞书 API Key 申请。", "The administrator has not enabled Feishu API Key requests.", language))
	}
	groups, err := s.apiKeyService.GetAvailableGroups(ctx, binding.UserID)
	if err != nil {
		return "", err
	}
	var group *Group
	for i := range groups {
		if groups[i].ID == groupID {
			group = &groups[i]
			break
		}
	}
	if group == nil {
		return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, localizeFeishu("当前账户无权申请该分组。", "This account is not eligible for the selected group.", language))
	}
	name := fmt.Sprintf("Feishu-%s", time.Now().Format("20060102"))
	request, inserted, err := s.apiKeyRequestRepo.Create(ctx, CreateFeishuAPIKeyRequestInput{
		UserID: binding.UserID, RequestedGroupID: groupID, RequestedName: name, SourceEventID: receipt.EventID,
	})
	if errors.Is(err, ErrFeishuAPIKeyRequestBusy) {
		return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, localizeFeishu("已有一个 API Key 申请正在处理中，请勿重复提交。", "An API Key request is already pending. Please do not submit another one.", language))
	}
	if err != nil {
		return "", err
	}
	if cfg.APIKeyRequestMode == FeishuAPIKeyRequestModeAuto && (inserted || request.Status == FeishuAPIKeyRequestStatusPending) {
		key, issueErr := s.issueFeishuAPIKeyRequest(ctx, request.ID, nil)
		if issueErr != nil {
			return "", issueErr
		}
		return s.enqueueFeishuBotCard(ctx, receipt, binding.UserID, s.feishuAPIKeyIssuedCard(ctx, key))
	}
	if !inserted {
		return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, localizeFeishu("该申请已经提交，无需重复操作。", "This request has already been submitted.", language))
	}
	return s.enqueueFeishuBotReply(ctx, receipt, binding.UserID, fmt.Sprintf(localizeFeishu("API Key 申请 #%d 已提交，管理员审批后会通过飞书通知你。", "API Key request #%d was submitted. You will receive a Feishu notification after review.", language), request.ID))
}

func (s *FeishuNotificationService) issueFeishuAPIKeyRequest(ctx context.Context, requestID int64, reviewedBy *int64) (*APIKey, error) {
	request, err := s.apiKeyRequestRepo.Claim(ctx, requestID)
	if err != nil {
		return nil, err
	}
	cfg, err := s.GetAssistantConfig(ctx)
	if err != nil {
		_ = s.apiKeyRequestRepo.ResetPending(ctx, request.ID, "configuration unavailable")
		return nil, err
	}
	_, page, err := s.apiKeyService.List(ctx, request.UserID, pagination.PaginationParams{Page: 1, PageSize: 1}, APIKeyListFilters{Status: StatusActive})
	if err != nil {
		_ = s.apiKeyRequestRepo.ResetPending(ctx, request.ID, "API key count failed")
		return nil, err
	}
	if cfg.MaxActiveKeys > 0 && page != nil && page.Total >= int64(cfg.MaxActiveKeys) {
		_ = s.apiKeyRequestRepo.ResetPending(ctx, request.ID, "active API key limit reached")
		return nil, infraerrors.Conflict("FEISHU_API_KEY_LIMIT_REACHED", "Active API key limit reached")
	}
	groupID := request.RequestedGroupID
	key, err := s.apiKeyService.Create(ctx, request.UserID, CreateAPIKeyRequest{Name: request.RequestedName, GroupID: &groupID})
	if err != nil {
		_ = s.apiKeyRequestRepo.ResetPending(ctx, request.ID, "API key creation failed")
		return nil, err
	}
	if err := s.apiKeyRequestRepo.MarkIssued(ctx, request.ID, key.ID, reviewedBy); err != nil {
		if deleteErr := s.apiKeyService.Delete(ctx, key.ID, request.UserID); deleteErr != nil {
			return nil, errors.Join(err, fmt.Errorf("remove orphaned API key: %w", deleteErr))
		}
		_ = s.apiKeyRequestRepo.ResetPending(ctx, request.ID, "API key issuance finalization failed")
		return nil, err
	}
	return key, nil
}

func (s *FeishuNotificationService) feishuAPIKeyIssuedCard(ctx context.Context, key *APIKey) map[string]any {
	name, suffix := "API Key", ""
	if key != nil {
		name = key.Name
		suffix = opaqueLastFour(key.Key)
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{"title": map[string]any{"tag": "plain_text", "content": "API Key 已创建 / API Key created"}, "template": "green"},
		"elements": []any{
			map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": fmt.Sprintf("%s · ****%s\n完整 API Key 不会发送到飞书，请在站内安全页面查看和复制。\nThe full API Key is never sent to Feishu. Open the secure account page to view and copy it.", name, suffix)}},
			s.feishuPanelActionElement(ctx, "打开安全页面 / Open secure page", ""),
		},
	}
}

func (s *FeishuNotificationService) ListFeishuAPIKeyRequests(ctx context.Context, status string, limit int) ([]FeishuAPIKeyRequest, error) {
	if s == nil || s.apiKeyRequestRepo == nil {
		return nil, fmt.Errorf("feishu API key request repository is unavailable")
	}
	return s.apiKeyRequestRepo.List(ctx, strings.TrimSpace(status), limit)
}

func (s *FeishuNotificationService) DecideFeishuAPIKeyRequest(ctx context.Context, requestID, actorID int64, approve bool, note string) (*FeishuAPIKeyRequest, error) {
	if s == nil || s.apiKeyRequestRepo == nil || actorID <= 0 {
		return nil, fmt.Errorf("feishu API key request service is unavailable")
	}
	if !approve {
		request, err := s.apiKeyRequestRepo.Reject(ctx, requestID, actorID, strings.TrimSpace(note))
		if err != nil {
			return nil, err
		}
		card := simpleFeishuAssistantCard("API Key 申请结果", fmt.Sprintf("申请 #%d 未通过。%s", request.ID, optionalFeishuReviewNote(request.ReviewNote)), "red")
		if err := s.queueBotCardForUser(ctx, request.UserID, fmt.Sprintf("api-key-request:%d:rejected", request.ID), card); err != nil {
			return nil, err
		}
		return request, nil
	}
	key, err := s.issueFeishuAPIKeyRequest(ctx, requestID, &actorID)
	if err != nil {
		return nil, err
	}
	request, err := s.apiKeyRequestRepo.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if err := s.queueBotCardForUser(ctx, request.UserID, fmt.Sprintf("api-key-request:%d:issued", request.ID), s.feishuAPIKeyIssuedCard(ctx, key)); err != nil {
		return nil, err
	}
	return request, nil
}

func (s *FeishuNotificationService) queueBotCardForUser(ctx context.Context, userID int64, businessKey string, card map[string]any) error {
	if s == nil || s.outboxRepo == nil || userID <= 0 {
		return fmt.Errorf("feishu bot delivery is unavailable")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	payload, err := marshalFeishuCard(card)
	if err != nil {
		return err
	}
	_, _, err = s.outboxRepo.Enqueue(ctx, FeishuNotificationOutboxInput{
		DedupeKey: "feishu:bot:" + strings.TrimSpace(businessKey), UserID: userID,
		AppID: cfg.AppID, Category: FeishuNotificationCategoryBotReply, Payload: payload,
	})
	return err
}

func marshalFeishuCard(card map[string]any) ([]byte, error) {
	return json.Marshal(card)
}

func simpleFeishuAssistantCard(title, content, template string) map[string]any {
	return map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": title}, "template": template},
		"elements": []any{map[string]any{"tag": "div", "text": map[string]any{"tag": "plain_text", "content": content}}},
	}
}

func optionalFeishuReviewNote(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return ""
	}
	return "\n说明：" + note
}
