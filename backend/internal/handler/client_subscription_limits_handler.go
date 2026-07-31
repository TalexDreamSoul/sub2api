package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type clientSubscriptionLimitsReader interface {
	Get(ctx context.Context, group *service.Group) (*service.ClientSubscriptionLimitsResponse, error)
}

type ClientSubscriptionLimitsHandler struct {
	service clientSubscriptionLimitsReader
}

func NewClientSubscriptionLimitsHandler(service *service.ClientSubscriptionLimitsService) *ClientSubscriptionLimitsHandler {
	return &ClientSubscriptionLimitsHandler{service: service}
}

func (h *ClientSubscriptionLimitsHandler) Get(c *gin.Context) {
	apiKey, authenticated := middleware.GetAPIKeyFromContext(c)
	if !authenticated || apiKey == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "API key authentication required"})
		return
	}

	limits, err := h.service.Get(c.Request.Context(), apiKey.Group)
	if errors.Is(err, service.ErrClientSubscriptionLimitsUnavailable) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscription limits are unavailable for this API key"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to retrieve subscription limits"})
		return
	}
	c.Header("Cache-Control", "private, max-age=60")
	c.JSON(http.StatusOK, limits)
}
