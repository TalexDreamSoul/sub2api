package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type feishuDiagnosticRequest struct {
	UserID   int64 `json:"user_id"`
	SendTest bool  `json:"send_test"`
}

func (h *SettingHandler) DiagnoseFeishuNotification(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu notification service is not configured")
		return
	}
	var req feishuDiagnosticRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.SendTest && !middleware2.EnforceStepUpAlways(c, h.totpService, h.userService) {
		return
	}
	report := h.feishuNotificationService.Diagnose(c.Request.Context(), req.UserID, req.SendTest)
	response.Success(c, report)
}

func (h *SettingHandler) ListFeishuBoundUsers(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu notification service is not configured")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	items, err := h.feishuNotificationService.ListBoundUsers(c.Request.Context(), c.Query("search"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *SettingHandler) ListFeishuNotificationDeliveries(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu notification service is not configured")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, err := h.feishuNotificationService.RecentDeliveries(c.Request.Context(), limit)
	if err != nil {
		response.InternalError(c, "failed to load feishu deliveries")
		return
	}
	response.Success(c, items)
}

type feishuAdminMessageRequest struct {
	UserID  int64  `json:"user_id"`
	Content string `json:"content"`
}

func (h *SettingHandler) SendFeishuAdminMessage(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu notification service is not configured")
		return
	}
	var req feishuAdminMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.UserID <= 0 || req.Content == "" {
		response.BadRequest(c, "user_id and content are required")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator session is required")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	payload := struct {
		UserID  int64  `json:"user_id"`
		Content string `json:"content"`
	}{UserID: req.UserID, Content: req.Content}

	executeAdminIdempotentJSON(c, "admin.feishu.messages.send", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		actorID := subject.UserID
		outboxID, inserted, err := h.feishuNotificationService.QueueAdminMessage(ctx, service.FeishuAdminMessageInput{
			UserID:         req.UserID,
			Content:        req.Content,
			IdempotencyKey: idempotencyKey,
			CreatedBy:      &actorID,
		})
		if err != nil {
			return nil, err
		}
		return gin.H{"queued": true, "inserted": inserted, "outbox_id": outboxID}, nil
	})
}

func (h *SettingHandler) GetFeishuAssistantConfig(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	cfg, err := h.feishuNotificationService.GetAssistantConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *SettingHandler) UpdateFeishuAssistantConfig(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	var req service.FeishuAssistantConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.feishuNotificationService.UpdateAssistantConfig(c.Request.Context(), req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, cfg)
}

func (h *SettingHandler) ListFeishuAPIKeyRequests(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	items, err := h.feishuNotificationService.ListFeishuAPIKeyRequests(c.Request.Context(), c.Query("status"), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

type decideFeishuAPIKeyRequest struct {
	Approve bool   `json:"approve"`
	Note    string `json:"note"`
}

func (h *SettingHandler) DecideFeishuAPIKeyRequest(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	requestID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || requestID <= 0 {
		response.BadRequest(c, "invalid request id")
		return
	}
	var req decideFeishuAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator session is required")
		return
	}
	item, err := h.feishuNotificationService.DecideFeishuAPIKeyRequest(c.Request.Context(), requestID, subject.UserID, req.Approve, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *SettingHandler) TestFeishuAssistantModel(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	if err := h.feishuNotificationService.TestAssistantModel(c.Request.Context()); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"function_calling": true})
}

type feishuAssistantAdminRequest struct {
	UserID int64 `json:"user_id"`
}

func (h *SettingHandler) ListFeishuAssistantAdmins(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	items, err := h.feishuNotificationService.ListFeishuAssistantAdmins(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *SettingHandler) AddFeishuAssistantAdmin(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	var req feishuAssistantAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID <= 0 {
		response.BadRequest(c, "valid user_id is required")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator session is required")
		return
	}
	if err := h.feishuNotificationService.AddFeishuAssistantAdmin(c.Request.Context(), subject.UserID, req.UserID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"assigned": true})
}

func (h *SettingHandler) RemoveFeishuAssistantAdmin(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "invalid user id")
		return
	}
	if err := h.feishuNotificationService.RemoveFeishuAssistantAdmin(c.Request.Context(), userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"removed": true})
}

func (h *SettingHandler) ListFeishuChatBindings(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	items, err := h.feishuNotificationService.ListFeishuChatBindings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

type updateFeishuChatBindingRequest struct {
	ChatName                     string `json:"chat_name"`
	Kind                         string `json:"kind"`
	Sub2APIGroupID               int64  `json:"sub2api_group_id"`
	IncidentNotificationsEnabled bool   `json:"incident_notifications_enabled"`
	DailyDigestEnabled           bool   `json:"daily_digest_enabled"`
}

func (h *SettingHandler) UpdateFeishuChatBinding(c *gin.Context) {
	if h.feishuNotificationService == nil {
		response.InternalError(c, "feishu assistant service is not configured")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid chat binding id")
		return
	}
	var req updateFeishuChatBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator session is required")
		return
	}
	item, err := h.feishuNotificationService.UpdateFeishuChatBinding(c.Request.Context(), subject.UserID, service.ConfigureFeishuChatInput{
		ID: id, ChatName: req.ChatName, Kind: req.Kind, Sub2APIGroupID: req.Sub2APIGroupID,
		IncidentNotificationsEnabled: req.IncidentNotificationsEnabled,
		DailyDigestEnabled:           req.DailyDigestEnabled,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}
