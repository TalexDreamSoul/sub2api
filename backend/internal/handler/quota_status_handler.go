package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type QuotaStatusHandler struct {
	service *service.QuotaStatusService
}

func NewQuotaStatusHandler(service *service.QuotaStatusService) *QuotaStatusHandler {
	return &QuotaStatusHandler{service: service}
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
	snapshot, err := h.service.GetSnapshot(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}
