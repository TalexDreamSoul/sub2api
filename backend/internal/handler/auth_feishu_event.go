package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) FeishuEventCallback(c *gin.Context) {
	if h == nil || h.feishuNotificationService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 1, "msg": "integration unavailable"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "invalid event"})
		return
	}
	result, err := h.feishuNotificationService.VerifyAndReceiveEvent(c.Request.Context(), service.FeishuEventHeaders{
		Timestamp: c.GetHeader("X-Lark-Request-Timestamp"),
		Nonce:     c.GetHeader("X-Lark-Request-Nonce"),
		Signature: c.GetHeader("X-Lark-Signature"),
	}, body)
	if err != nil {
		if errors.Is(err, service.ErrFeishuEventUnauthorized) || errors.Is(err, service.ErrFeishuEventInvalid) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 1, "msg": "event verification failed"})
			return
		}
		slog.Error("persist feishu event failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 1, "msg": "temporary failure"})
		return
	}
	if result.Challenge != "" {
		c.JSON(http.StatusOK, gin.H{"challenge": result.Challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}
