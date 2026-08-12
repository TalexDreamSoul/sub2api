package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ContentModerationHandler struct {
	service      *service.ContentModerationService
	adminService service.AdminService
}

func NewContentModerationHandler(svc *service.ContentModerationService, adminService service.AdminService) *ContentModerationHandler {
	return &ContentModerationHandler{service: svc, adminService: adminService}
}

type contentModerationConfigRequest struct {
	Enabled *bool   `json:"enabled"`
	Mode    *string `json:"mode"`
	BaseURL *string `json:"base_url"`
	Model   *string `json:"model"`
	// 审计请求使用的代理服务器：null 不修改；0 清除（直连）；>0 指定代理。
	ProxyID                             *int64                                       `json:"proxy_id"`
	APIKey                              *string                                      `json:"api_key"`
	APIKeys                             *[]string                                    `json:"api_keys"`
	APIKeysMode                         string                                       `json:"api_keys_mode"`
	DeleteAPIKeyHashes                  *[]string                                    `json:"delete_api_key_hashes"`
	ClearAPIKey                         bool                                         `json:"clear_api_key"`
	TimeoutMS                           *int                                         `json:"timeout_ms"`
	SampleRate                          *int                                         `json:"sample_rate"`
	AllGroups                           *bool                                        `json:"all_groups"`
	GroupIDs                            *[]int64                                     `json:"group_ids"`
	RecordNonHits                       *bool                                        `json:"record_non_hits"`
	Thresholds                          *map[string]float64                          `json:"thresholds"`
	WorkerCount                         *int                                         `json:"worker_count"`
	QueueSize                           *int                                         `json:"queue_size"`
	BlockStatus                         *int                                         `json:"block_status"`
	BlockMessage                        *string                                      `json:"block_message"`
	EmailOnHit                          *bool                                        `json:"email_on_hit"`
	AutoBanEnabled                      *bool                                        `json:"auto_ban_enabled"`
	BanThreshold                        *int                                         `json:"ban_threshold"`
	BanDurationMinutes                  *int                                         `json:"ban_duration_minutes"`
	ViolationWindowHours                *int                                         `json:"violation_window_hours"`
	RetryCount                          *int                                         `json:"retry_count"`
	HitRetentionDays                    *int                                         `json:"hit_retention_days"`
	NonHitRetentionDays                 *int                                         `json:"non_hit_retention_days"`
	ContextRetentionDays                *int                                         `json:"context_retention_days"`
	PreHashCheckEnabled                 *bool                                        `json:"pre_hash_check_enabled"`
	BlockedKeywords                     *[]string                                    `json:"blocked_keywords"`
	KeywordBlockingMode                 *string                                      `json:"keyword_blocking_mode"`
	KeywordRules                        *[]service.ContentModerationKeywordRule      `json:"keyword_rules"`
	ModelFilter                         *service.ContentModerationModelFilter        `json:"model_filter"`
	AuditModels                         *[]service.ContentModerationAuditModelConfig `json:"audit_models"`
	DecisionRule                        *service.ContentModerationDecisionRule       `json:"decision_rule"`
	SelfUnban                           *service.ContentModerationSelfUnbanConfig    `json:"self_unban"`
	RiskWeightEnabled                   *bool                                        `json:"risk_weight_enabled"`
	FlaggedWeight                       *float64                                     `json:"flagged_weight"`
	BanWeight                           *float64                                     `json:"ban_weight"`
	ManualSuspiciousWeight              *float64                                     `json:"manual_suspicious_weight"`
	DecayHalfLifeDays                   *int                                         `json:"decay_half_life_days"`
	MaxSampleRate                       *int                                         `json:"max_sample_rate"`
	BanThresholdWeightStep              *int                                         `json:"ban_threshold_weight_step"`
	MinEffectiveBanThreshold            *int                                         `json:"min_effective_ban_threshold"`
	BackgroundReviewEnabled             *bool                                        `json:"background_review_enabled"`
	BackgroundReviewBatchSize           *int                                         `json:"background_review_batch_size"`
	BackgroundReviewMaxAttempts         *int                                         `json:"background_review_max_attempts"`
	BackgroundReviewRetryBackoffSeconds *int                                         `json:"background_review_retry_backoff_seconds"`
	ContextCaptureEnabled               *bool                                        `json:"context_capture_enabled"`
	ContextMaxBytes                     *int                                         `json:"context_max_bytes"`
	CyberuseResponse                    *service.ContentModerationCyberuseConfig     `json:"cyberuse_response"`
	CyberPolicyExcludeFromBanCount      *bool                                        `json:"cyber_policy_exclude_from_ban_count"`
}

type contentModerationAPIKeyTestRequest struct {
	APIKeys   []string `json:"api_keys"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	TimeoutMS int      `json:"timeout_ms"`
	ProxyID   *int64   `json:"proxy_id"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
}

type contentModerationHashRequest struct {
	InputHash string `json:"input_hash"`
}

type contentModerationSuspicionRequest struct {
	Suspicious bool   `json:"suspicious"`
	Reason     string `json:"reason"`
}

type requestRiskConfigRequest = service.UpdateRequestRiskControlConfigInput

func (h *ContentModerationHandler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *ContentModerationHandler) UpdateConfig(c *gin.Context) {
	var req contentModerationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.service.UpdateConfig(c.Request.Context(), service.UpdateContentModerationConfigInput{
		Enabled:                             req.Enabled,
		Mode:                                req.Mode,
		BaseURL:                             req.BaseURL,
		Model:                               req.Model,
		ProxyID:                             req.ProxyID,
		CyberPolicyExcludeFromBanCount:      req.CyberPolicyExcludeFromBanCount,
		APIKey:                              req.APIKey,
		APIKeys:                             req.APIKeys,
		APIKeysMode:                         req.APIKeysMode,
		DeleteAPIKeyHashes:                  req.DeleteAPIKeyHashes,
		ClearAPIKey:                         req.ClearAPIKey,
		TimeoutMS:                           req.TimeoutMS,
		SampleRate:                          req.SampleRate,
		AllGroups:                           req.AllGroups,
		GroupIDs:                            req.GroupIDs,
		RecordNonHits:                       req.RecordNonHits,
		Thresholds:                          req.Thresholds,
		WorkerCount:                         req.WorkerCount,
		QueueSize:                           req.QueueSize,
		BlockStatus:                         req.BlockStatus,
		BlockMessage:                        req.BlockMessage,
		EmailOnHit:                          req.EmailOnHit,
		AutoBanEnabled:                      req.AutoBanEnabled,
		BanThreshold:                        req.BanThreshold,
		BanDurationMinutes:                  req.BanDurationMinutes,
		ViolationWindowHours:                req.ViolationWindowHours,
		RetryCount:                          req.RetryCount,
		HitRetentionDays:                    req.HitRetentionDays,
		NonHitRetentionDays:                 req.NonHitRetentionDays,
		ContextRetentionDays:                req.ContextRetentionDays,
		PreHashCheckEnabled:                 req.PreHashCheckEnabled,
		BlockedKeywords:                     req.BlockedKeywords,
		KeywordBlockingMode:                 req.KeywordBlockingMode,
		KeywordRules:                        req.KeywordRules,
		ModelFilter:                         req.ModelFilter,
		AuditModels:                         req.AuditModels,
		DecisionRule:                        req.DecisionRule,
		SelfUnban:                           req.SelfUnban,
		RiskWeightEnabled:                   req.RiskWeightEnabled,
		FlaggedWeight:                       req.FlaggedWeight,
		BanWeight:                           req.BanWeight,
		ManualSuspiciousWeight:              req.ManualSuspiciousWeight,
		DecayHalfLifeDays:                   req.DecayHalfLifeDays,
		MaxSampleRate:                       req.MaxSampleRate,
		BanThresholdWeightStep:              req.BanThresholdWeightStep,
		MinEffectiveBanThreshold:            req.MinEffectiveBanThreshold,
		BackgroundReviewEnabled:             req.BackgroundReviewEnabled,
		BackgroundReviewBatchSize:           req.BackgroundReviewBatchSize,
		BackgroundReviewMaxAttempts:         req.BackgroundReviewMaxAttempts,
		BackgroundReviewRetryBackoffSeconds: req.BackgroundReviewRetryBackoffSeconds,
		ContextCaptureEnabled:               req.ContextCaptureEnabled,
		ContextMaxBytes:                     req.ContextMaxBytes,
		CyberuseResponse:                    req.CyberuseResponse})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *ContentModerationHandler) TestAPIKeys(c *gin.Context) {
	var req contentModerationAPIKeyTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.TestAPIKeys(c.Request.Context(), service.TestContentModerationAPIKeysInput{
		APIKeys:   req.APIKeys,
		BaseURL:   req.BaseURL,
		Model:     req.Model,
		TimeoutMS: req.TimeoutMS,
		ProxyID:   req.ProxyID,
		Prompt:    req.Prompt,
		Images:    req.Images,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) GetRequestRiskConfig(c *gin.Context) {
	cfg, err := h.service.GetRequestRiskControlConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *ContentModerationHandler) UpdateRequestRiskConfig(c *gin.Context) {
	var req requestRiskConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.service.UpdateRequestRiskControlConfig(c.Request.Context(), service.UpdateRequestRiskControlConfigInput(req))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *ContentModerationHandler) ListRequestRiskEvents(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := service.RequestRiskEventFilter{
		PaginationParams: pagination.PaginationParams{Page: page, PageSize: pageSize},
		Action:           strings.TrimSpace(c.Query("action")),
		Query:            strings.TrimSpace(c.Query("q")),
		Rule:             strings.TrimSpace(c.Query("rule")),
	}
	if v := strings.TrimSpace(c.Query("api_key_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		filter.APIKeyID = &id
	}
	if v := strings.TrimSpace(c.Query("user_id")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		filter.UserID = &id
	}
	if v := strings.TrimSpace(c.Query("from")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.BadRequest(c, "Invalid from")
			return
		}
		filter.From = &t
	}
	if v := strings.TrimSpace(c.Query("to")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			response.BadRequest(c, "Invalid to")
			return
		}
		filter.To = &t
	}
	items, pageResult, err := h.service.ListRequestRiskEvents(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"items":     items,
		"total":     pageResult.Total,
		"page":      pageResult.Page,
		"page_size": pageResult.PageSize,
		"pages":     pageResult.Pages,
	})
}

func (h *ContentModerationHandler) GetRequestRiskEvent(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid request risk event id")
		return
	}
	item, err := h.service.GetRequestRiskEvent(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *ContentModerationHandler) GetStatus(c *gin.Context) {
	status, err := h.service.GetStatus(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

func (h *ContentModerationHandler) ListLogs(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := service.ContentModerationLogFilter{
		Pagination: pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortOrder: pagination.SortOrderDesc,
		},
		Result:   c.Query("result"),
		Endpoint: c.Query("endpoint"),
		Search:   c.Query("search"),
	}
	if raw := strings.TrimSpace(c.Query("group_id")); raw != "" {
		groupID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || groupID <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filter.GroupID = &groupID
	}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		t, _, err := parseContentModerationDate(raw)
		if err != nil {
			response.BadRequest(c, "Invalid from")
			return
		}
		filter.From = &t
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, dateOnly, err := parseContentModerationDate(raw)
		if err != nil {
			response.BadRequest(c, "Invalid to")
			return
		}
		if dateOnly {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		filter.To = &t
	}
	items, pageResult, err := h.service.ListLogs(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, pageResult.Total, pageResult.Page, pageResult.PageSize)
}

func (h *ContentModerationHandler) GetUserBanStatus(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	result, err := h.service.GetUserBanStatus(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) GetUserRiskProfile(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	result, err := h.service.GetUserRiskDetail(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) SetUserSuspicion(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	var req contentModerationSuspicionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if _, ok := requireManageableUser(c, h.adminService, userID); !ok {
		return
	}
	result, err := h.service.SetUserManualSuspicious(c.Request.Context(), userID, req.Suspicious, req.Reason)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) ListUserContexts(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	result, err := h.service.ListUserContexts(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) GetContextDetail(c *gin.Context) {
	contextID, err := strconv.ParseInt(strings.TrimSpace(c.Param("context_id")), 10, 64)
	if err != nil || contextID <= 0 {
		response.BadRequest(c, "Invalid context_id")
		return
	}
	var adminUserID int64
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		adminUserID = subject.UserID
	}
	result, err := h.service.GetContextDetail(c.Request.Context(), contextID, adminUserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) SelfUnban(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	if _, ok := requireManageableUser(c, h.adminService, userID); !ok {
		return
	}
	result, err := h.service.SelfUnbanUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) UnbanUser(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	if _, ok := requireManageableUser(c, h.adminService, userID); !ok {
		return
	}
	result, err := h.service.UnbanUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) DeleteFlaggedHash(c *gin.Context) {
	var req contentModerationHashRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.DeleteFlaggedInputHash(c.Request.Context(), req.InputHash)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) ClearFlaggedHashes(c *gin.Context) {
	result, err := h.service.ClearFlaggedInputHashes(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func parseContentModerationDate(raw string) (time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, false, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	return t, err == nil, err
}
