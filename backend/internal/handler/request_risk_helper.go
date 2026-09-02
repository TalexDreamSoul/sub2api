package handler

import (
	"net/http"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *GatewayHandler) checkRequestRisk(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.RequestRiskDecision {
	if h == nil || h.contentModerationService == nil {
		return nil
	}
	return runRequestRisk(c, reqLog, h.contentModerationService, nil, apiKey, subject, protocol, model, body)
}

func runRequestRisk(c *gin.Context, reqLog *zap.Logger, svc *service.ContentModerationService, openAISvc *service.OpenAIGatewayService, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.RequestRiskDecision {
	if svc == nil || c == nil || c.Request == nil {
		return nil
	}
	if service.IsContentModerationInternalAuditAPIKey(apiKey) {
		return nil
	}
	base := buildContentModerationInput(c, apiKey, subject, protocol, model, body)
	input := service.RequestRiskEvaluationInput{
		RequestID:   base.RequestID,
		UserID:      base.UserID,
		APIKeyID:    base.APIKeyID,
		RequestPath: base.Endpoint,
		Model:       base.Model,
		Headers:     c.Request.Header,
		Body:        body,
		Protocol:    protocol,
	}
	if openAISvc != nil && apiKey != nil {
		input.CyberSessionKey = service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
		if input.CyberSessionKey == "" {
			if keys := service.CyberSessionTranscriptBlockKeys(apiKey.ID, body); len(keys) > 0 {
				input.CyberSessionKey = keys[0]
			}
		}
		input.SessionBlocked = openAISvc.IsCyberSessionBlockedRaw(c.Request.Context(), input.CyberSessionKey)
	}
	decision, err := svc.EvaluateRequestRisk(c.Request.Context(), input)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("request_risk.check_failed", zap.Error(err))
		}
		return nil
	}
	if reqLog != nil && decision != nil && len(decision.MatchedRules) > 0 {
		reqLog.Info("request_risk.check_done",
			zap.String("request_id", input.RequestID),
			zap.Int64("user_id", input.UserID),
			zap.Int64("api_key_id", input.APIKeyID),
			zap.String("model", input.Model),
			zap.Bool("blocked", decision.Blocked),
			zap.String("action", decision.Action),
			zap.Strings("matched_rules", decision.MatchedRules),
		)
	}
	return decision
}

func requestRiskStatus(decision *service.RequestRiskDecision) int {
	if decision == nil || decision.StatusCode < 400 || decision.StatusCode > 599 {
		return http.StatusForbidden
	}
	return decision.StatusCode
}

func requestRiskErrorCode(decision *service.RequestRiskDecision) string {
	if decision != nil && decision.ErrorCode != "" {
		return decision.ErrorCode
	}
	return "cyber_policy"
}

func requestRiskMessage(decision *service.RequestRiskDecision) string {
	if decision != nil && decision.Message != "" {
		return decision.Message
	}
	return "Your request was blocked by local content moderation policy."
}

func requestRiskAsContentModerationDecision(decision *service.RequestRiskDecision) *service.ContentModerationDecision {
	if decision == nil || !decision.Blocked {
		return nil
	}
	return &service.ContentModerationDecision{
		Allowed:    false,
		Blocked:    true,
		Flagged:    true,
		Message:    requestRiskMessage(decision),
		ErrorCode:  requestRiskErrorCode(decision),
		StatusCode: requestRiskStatus(decision),
		Action:     decision.Action,
	}
}
