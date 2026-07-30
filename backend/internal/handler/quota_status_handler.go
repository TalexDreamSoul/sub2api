package handler

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type quotaStatusGroupAccess interface {
	GetAvailableGroups(ctx context.Context, userID int64) ([]service.Group, error)
}

type QuotaStatusHandler struct {
	service     *service.QuotaStatusService
	groupAccess quotaStatusGroupAccess
}

func NewQuotaStatusHandler(quotaService *service.QuotaStatusService, groupAccess *service.APIKeyService) *QuotaStatusHandler {
	return &QuotaStatusHandler{service: quotaService, groupAccess: groupAccess}
}

func (h *QuotaStatusHandler) GetConfig(c *gin.Context) {
	config, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

func (h *QuotaStatusHandler) UpdateConfig(c *gin.Context) {
	var input service.QuotaStatusConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "invalid quota status config")
		return
	}
	config, err := h.service.UpdateConfig(c.Request.Context(), input)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, config)
}

func (h *QuotaStatusHandler) GetPublic(c *gin.Context) {
	ctx := c.Request.Context()
	config, err := h.service.GetConfig(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	var visibleGroupIDs map[int64]struct{}
	if config.Enabled && config.AccessMode != service.QuotaStatusAccessModePublic {
		subject, authenticated := servermiddleware.GetAuthSubjectFromContext(c)
		if !authenticated {
			response.Unauthorized(c, "Authentication required")
			return
		}

		if config.AccessMode == service.QuotaStatusAccessModeGroupScoped {
			role, _ := servermiddleware.GetUserRoleFromContext(c)
			if role != service.RoleAdmin {
				if h.groupAccess == nil {
					response.ErrorFrom(c, errors.New("quota status group access service is unavailable"))
					return
				}
				groups, groupErr := h.groupAccess.GetAvailableGroups(ctx, subject.UserID)
				if groupErr != nil {
					response.ErrorFrom(c, groupErr)
					return
				}
				visibleGroupIDs = make(map[int64]struct{}, len(groups))
				for _, group := range groups {
					visibleGroupIDs[group.ID] = struct{}{}
				}
			}
		}
	}

	snapshot, err := h.service.GetSnapshotForGroups(ctx, visibleGroupIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}
