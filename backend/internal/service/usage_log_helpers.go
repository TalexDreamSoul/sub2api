package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

func optionalTrimmedStringPtr(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func forwardResultBillingModel(requestedModel, upstreamModel string) string {
	if trimmed := strings.TrimSpace(requestedModel); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(upstreamModel)
}

func OptionalInt64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func optionalInt64Ptr(v int64) *int64 {
	return OptionalInt64Ptr(v)
}

func WithSpeedUsageMetadata(ctx context.Context, state string, waitMs int, route string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if state = strings.TrimSpace(state); state != "" {
		ctx = context.WithValue(ctx, ctxkey.SpeedState, state)
	}
	if waitMs < 0 {
		waitMs = 0
	}
	if waitMs > 0 {
		ctx = context.WithValue(ctx, ctxkey.SpeedWaitMs, waitMs)
	}
	if route = strings.TrimSpace(route); route != "" {
		ctx = context.WithValue(ctx, ctxkey.SpeedRoute, route)
	}
	return ctx
}

func ApplySpeedUsageMetadataFromContext(ctx context.Context, log *UsageLog) {
	if ctx == nil || log == nil {
		return
	}
	if state, _ := ctx.Value(ctxkey.SpeedState).(string); strings.TrimSpace(state) != "" {
		log.SpeedState = optionalTrimmedStringPtr(state)
	}
	switch v := ctx.Value(ctxkey.SpeedWaitMs).(type) {
	case int:
		if v > 0 {
			log.SpeedWaitMs = v
		}
	case int64:
		if v > 0 {
			log.SpeedWaitMs = int(v)
		}
	case float64:
		if v > 0 {
			log.SpeedWaitMs = int(v)
		}
	}
	if route, _ := ctx.Value(ctxkey.SpeedRoute).(string); strings.TrimSpace(route) != "" {
		log.SpeedRoute = optionalTrimmedStringPtr(route)
	}
}
