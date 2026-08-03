package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

type FeishuDiagnosticStep struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

type FeishuDiagnosticReport struct {
	Healthy bool                          `json:"healthy"`
	AppID   string                        `json:"app_id,omitempty"`
	Steps   []FeishuDiagnosticStep        `json:"steps"`
	Outbox  FeishuNotificationOutboxStats `json:"outbox"`
}

func (s *FeishuNotificationService) RecentDeliveries(ctx context.Context, limit int) ([]FeishuNotificationDelivery, error) {
	if s == nil || s.outboxRepo == nil {
		return nil, fmt.Errorf("notification outbox is unavailable")
	}
	return s.outboxRepo.ListRecent(ctx, limit)
}

// Diagnose returns a complete report even when an earlier step fails so operators can fix all
// configuration problems in one pass.
func (s *FeishuNotificationService) Diagnose(ctx context.Context, userID int64, sendTest bool) FeishuDiagnosticReport {
	report := FeishuDiagnosticReport{Healthy: true, Steps: make([]FeishuDiagnosticStep, 0, 8)}
	cfgStarted := time.Now()
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		report.addDiagnosticError("config", err, cfgStarted)
		return report
	}
	report.AppID = cfg.AppID
	if !cfg.Enabled || cfg.AppID == "" || cfg.AppSecret == "" {
		report.addDiagnostic("config", "failed", "notification app is disabled or credentials are incomplete", cfgStarted)
	} else {
		report.addDiagnostic("config", "passed", "notification app configuration is complete", cfgStarted)
	}

	eventSecurityStarted := time.Now()
	if strings.TrimSpace(cfg.VerificationToken) == "" {
		report.addDiagnostic("event_security", "failed", "verification token is required for event callbacks", eventSecurityStarted)
	} else if strings.TrimSpace(cfg.EncryptKey) == "" {
		report.addDiagnostic("event_security", "warning", "verification token is configured; event encryption is disabled", eventSecurityStarted)
	} else {
		report.addDiagnostic("event_security", "passed", "verification token and event encryption are configured", eventSecurityStarted)
	}

	tokenStarted := time.Now()
	if _, err := s.fetchTenantAccessToken(ctx, cfg); err != nil {
		report.addDiagnosticError("tenant_token", err, tokenStarted)
	} else {
		report.addDiagnostic("tenant_token", "passed", "tenant token acquired", tokenStarted)
	}

	bindingStarted := time.Now()
	if userID <= 0 {
		report.addDiagnostic("binding", "warning", "no test user selected", bindingStarted)
	} else if s == nil || s.bindingRepo == nil {
		report.addDiagnostic("binding", "failed", "binding repository is unavailable", bindingStarted)
	} else if binding, err := s.bindingRepo.GetFeishuNotificationBinding(ctx, userID, cfg.AppID); err != nil {
		report.addDiagnosticError("binding", err, bindingStarted)
	} else if !binding.NotificationEnabled {
		report.addDiagnostic("binding", "warning", "user is bound but notifications are disabled", bindingStarted)
	} else {
		report.addDiagnostic("binding", "passed", "test user binding is active", bindingStarted)
	}

	panelStarted := time.Now()
	if strings.TrimSpace(cfg.PanelURL) == "" {
		report.addDiagnostic("panel_url", "failed", "panel URL is empty", panelStarted)
	} else {
		report.addDiagnostic("panel_url", "passed", "panel URL is configured", panelStarted)
	}

	outboxStarted := time.Now()
	if s == nil || s.outboxRepo == nil {
		report.addDiagnostic("outbox", "failed", "notification outbox is unavailable", outboxStarted)
	} else if stats, err := s.outboxRepo.Stats(ctx); err != nil {
		report.addDiagnosticError("outbox", err, outboxStarted)
	} else {
		report.Outbox = stats
		status := "passed"
		message := fmt.Sprintf("pending=%d processing=%d dead=%d", stats.Pending, stats.Processing, stats.Dead)
		if stats.Dead > 0 {
			status = "warning"
		}
		report.addDiagnostic("outbox", status, message, outboxStarted)
	}

	if sendTest {
		sendStarted := time.Now()
		if userID <= 0 {
			report.addDiagnostic("test_message", "failed", "test user is required", sendStarted)
		} else if err := s.SendTest(ctx, userID); err != nil {
			report.addDiagnosticError("test_message", err, sendStarted)
		} else {
			report.addDiagnostic("test_message", "passed", "test card delivered", sendStarted)
		}
	}
	return report
}

func (r *FeishuDiagnosticReport) addDiagnostic(name, status, message string, started time.Time) {
	r.addDiagnosticWithDetail(name, status, message, "", started)
}

func (r *FeishuDiagnosticReport) addDiagnosticError(name string, err error, started time.Time) {
	r.addDiagnosticWithDetail(name, "failed", diagnosticError(err), diagnosticErrorDetail(err), started)
}

func (r *FeishuDiagnosticReport) addDiagnosticWithDetail(name, status, message, detail string, started time.Time) {
	if status == "failed" {
		r.Healthy = false
	}
	r.Steps = append(r.Steps, FeishuDiagnosticStep{
		Name:      name,
		Status:    status,
		Message:   message,
		Detail:    detail,
		LatencyMS: time.Since(started).Milliseconds(),
	})
}

func diagnosticError(err error) string {
	if err == nil {
		return ""
	}
	if reason := infraerrors.Reason(err); reason != "" {
		return reason
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "request_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "request_canceled"
	}
	return "integration_request_failed"
}

const maxFeishuDiagnosticDetailRunes = 512

func diagnosticErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *feishuNotifyAPIError
	if errors.As(err, &apiErr) {
		message := logredact.RedactText(apiErr.Message, "api_key", "apikey", "token", "secret", "key")
		return truncateFeishuDiagnosticDetail(fmt.Sprintf("%s status=%d code=%s msg=%s",
			apiErr.Operation, apiErr.Status, apiErr.Code, message))
	}
	if infraerrors.Reason(err) != "" {
		return truncateFeishuDiagnosticDetail(infraerrors.Message(err))
	}
	detail := logredact.RedactText(err.Error(), "api_key", "apikey", "token", "secret", "key")
	return truncateFeishuDiagnosticDetail(detail)
}

func truncateFeishuDiagnosticDetail(detail string) string {
	detail = strings.TrimSpace(strings.ToValidUTF8(detail, ""))
	runes := []rune(detail)
	if len(runes) > maxFeishuDiagnosticDetailRunes {
		return string(runes[:maxFeishuDiagnosticDetailRunes-1]) + "…"
	}
	return detail
}
