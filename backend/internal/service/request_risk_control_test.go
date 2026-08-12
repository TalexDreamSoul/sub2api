package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type requestRiskTestRepo struct {
	events []RequestRiskEvent
	uaBans map[string]RequestRiskUserAgentBan
}

func (r *requestRiskTestRepo) CreateLog(context.Context, *ContentModerationLog) error { return nil }
func (r *requestRiskTestRepo) ListLogs(context.Context, ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *requestRiskTestRepo) CountFlaggedByUserSince(context.Context, int64, time.Time, bool) (int, error) {
	return 0, nil
}
func (r *requestRiskTestRepo) CleanupExpiredLogs(context.Context, time.Time, time.Time, time.Time) (*ContentModerationCleanupResult, error) {
	return &ContentModerationCleanupResult{}, nil
}
func (r *requestRiskTestRepo) UpsertUserBan(context.Context, *ContentModerationUserBan) error {
	return nil
}
func (r *requestRiskTestRepo) GetActiveUserBan(context.Context, int64, time.Time) (*ContentModerationUserBan, error) {
	return nil, nil
}
func (r *requestRiskTestRepo) ClearUserBan(context.Context, int64, time.Time) error { return nil }
func (r *requestRiskTestRepo) CountSelfUnbanAttempts(context.Context, int64, time.Time) (int, error) {
	return 0, nil
}
func (r *requestRiskTestRepo) CreateSelfUnbanRecord(context.Context, *ContentModerationSelfUnbanRecord) error {
	return nil
}
func (r *requestRiskTestRepo) GetUserRiskProfile(context.Context, int64) (*ContentModerationUserRiskProfile, error) {
	return nil, nil
}
func (r *requestRiskTestRepo) UpsertUserRiskProfile(context.Context, *ContentModerationUserRiskProfile) error {
	return nil
}
func (r *requestRiskTestRepo) CreateUserRiskEvent(context.Context, *ContentModerationUserRiskEvent) error {
	return nil
}
func (r *requestRiskTestRepo) ListUserRiskEvents(context.Context, int64, int) ([]ContentModerationUserRiskEvent, error) {
	return nil, nil
}
func (r *requestRiskTestRepo) CreateContext(context.Context, *ContentModerationContext) error {
	return nil
}
func (r *requestRiskTestRepo) ClaimPendingContexts(context.Context, int) ([]ContentModerationContext, error) {
	return nil, nil
}
func (r *requestRiskTestRepo) UpdateContextReview(context.Context, ContentModerationContextReviewUpdate) error {
	return nil
}
func (r *requestRiskTestRepo) CountContextsByStatus(context.Context) (*ContentModerationContextStatusCounts, error) {
	return &ContentModerationContextStatusCounts{}, nil
}
func (r *requestRiskTestRepo) ListUserContexts(context.Context, int64, int) ([]ContentModerationContext, error) {
	return nil, nil
}
func (r *requestRiskTestRepo) GetContextByID(context.Context, int64) (*ContentModerationContext, error) {
	return nil, nil
}
func (r *requestRiskTestRepo) CreateContextAccessLog(context.Context, int64, int64, string) error {
	return nil
}
func (r *requestRiskTestRepo) UpdateLogEmailSent(context.Context, int64, bool) error { return nil }

func (r *requestRiskTestRepo) CreateRequestRiskEvent(ctx context.Context, event *RequestRiskEvent) error {
	r.events = append(r.events, *event)
	return nil
}
func (r *requestRiskTestRepo) ListRequestRiskEvents(context.Context, RequestRiskEventFilter) ([]RequestRiskEvent, *pagination.PaginationResult, error) {
	return r.events, &pagination.PaginationResult{Total: int64(len(r.events)), Page: 1, PageSize: 20, Pages: 1}, nil
}
func (r *requestRiskTestRepo) GetRequestRiskEvent(context.Context, int64) (*RequestRiskEvent, error) {
	if len(r.events) == 0 {
		return nil, nil
	}
	return &r.events[0], nil
}
func (r *requestRiskTestRepo) CleanupExpiredRequestRiskEvents(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *requestRiskTestRepo) UpsertRequestRiskUABan(ctx context.Context, ban *RequestRiskUserAgentBan) error {
	if r.uaBans == nil {
		r.uaBans = map[string]RequestRiskUserAgentBan{}
	}
	r.uaBans[ban.UserAgentHash] = *ban
	return nil
}
func (r *requestRiskTestRepo) GetActiveRequestRiskUABan(ctx context.Context, apiKeyID int64, userAgentHash string, now time.Time) (*RequestRiskUserAgentBan, error) {
	if r.uaBans == nil {
		return nil, nil
	}
	ban, ok := r.uaBans[userAgentHash]
	if !ok || ban.APIKeyID != apiKeyID || !ban.BannedUntil.After(now) {
		return nil, nil
	}
	return &ban, nil
}

func requestRiskTestService(repo *requestRiskTestRepo) *ContentModerationService {
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyRequestRiskControlEnabled:              "true",
		SettingKeyRequestRiskControlMode:                 RequestRiskControlModeEnforce,
		SettingKeyRequestRiskControlWindowsEnhanced:      "true",
		SettingKeyRequestRiskControlDeniedTimezones:      `["Asia/Shanghai","Asia/Urumqi"]`,
		SettingKeyRequestRiskControlChineseHighThreshold: "0.45",
		SettingKeyRequestRiskControlEventRetentionDays:   "7",
		SettingKeyRequestRiskControlCaptureRawHeaders:    "true",
		SettingKeyRequestRiskControlUABanScope:           RequestRiskUABanScopeAPIKey,
		SettingKeyRequestRiskControlSessionBanTTLSeconds: "1800",
		SettingKeyRequestRiskControlUABanTTLSeconds:      "1800",
	}}
	return NewContentModerationService(settings, repo, nil, nil, nil, nil, nil, nil)
}

func TestRequestRiskParsers(t *testing.T) {
	require.Equal(t, "windows", RequestRiskClientPlatform("Mozilla/5.0 (Windows NT 10.0; Win64; x64)"))
	require.Equal(t, "macos", RequestRiskClientPlatform("Codex/1.0 Darwin arm64"))
	require.Equal(t, "linux", RequestRiskClientPlatform("Mozilla/5.0 (X11; Linux x86_64)"))

	body := []byte(`{"input":[{"type":"message","content":[{"type":"input_text","text":"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"}]}]}`)
	require.Equal(t, "Asia/Shanghai", ExtractRequestRiskTimezoneFromResponsesBody(body))

	require.Equal(t, "CN", ParseRequestRiskXFoo(`{"inference_geo":"CN","timezone":"Asia/Shanghai"}`)["inference_geo"])
	require.Equal(t, "Asia/Shanghai", ParseRequestRiskXFoo(`inference_geo=CN&timezone=Asia%2FShanghai`)["timezone"])
	require.Equal(t, "CN", ParseRequestRiskXFoo(`inference_geo=CN; timezone=Asia/Shanghai`)["inference_geo"])

	require.Greater(t, RequestRiskChineseIntensity("你好，请帮我处理这个请求"), 0.8)
	require.Less(t, RequestRiskChineseIntensity("hello world"), 0.1)
}

func TestRequestRiskWindowsInferenceGeoCNRejects(t *testing.T) {
	repo := &requestRiskTestRepo{}
	svc := requestRiskTestService(repo)
	decision, err := svc.EvaluateRequestRisk(context.Background(), RequestRiskEvaluationInput{
		RequestID:       "req-1",
		UserID:          10,
		APIKeyID:        20,
		CyberSessionKey: "session-key",
		Headers: http.Header{
			"User-Agent":    []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
			"Inference-Geo": []string{"CN"},
		},
		Body: []byte(`{"prompt_cache_key":"sess-1","input":[{"content":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, RequestRiskActionBanUserAgent, decision.Action)
	require.Contains(t, decision.MatchedRules, RequestRiskRuleWindowsInferenceGeoCN)
	require.Len(t, repo.events, 1)
	require.Equal(t, []string{"CN"}, repo.events[0].RawHeaders["Inference-Geo"])
	require.Len(t, repo.uaBans, 1)
}

func TestRequestRiskDeniedTimezoneAndHighChineseBanSession(t *testing.T) {
	repo := &requestRiskTestRepo{}
	svc := requestRiskTestService(repo)
	body := []byte(`{"prompt_cache_key":"sess-2","input":[{"content":[{"text":"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>你好，请继续处理中文请求"}]}]}`)
	decision, err := svc.EvaluateRequestRisk(context.Background(), RequestRiskEvaluationInput{
		RequestID:       "req-2",
		UserID:          10,
		APIKeyID:        20,
		CyberSessionKey: "session-key-2",
		Headers: http.Header{
			"User-Agent":      []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
			"Accept-Language": []string{"zh-CN,zh;q=0.9"},
		},
		Body: body,
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, RequestRiskActionBanSession, decision.Action)
	require.Equal(t, "session-key-2", decision.SessionBanKey)
	require.Equal(t, 1800, decision.SessionBanTTLSeconds)
	require.Contains(t, decision.MatchedRules, RequestRiskRuleWindowsDeniedTimezone)
	require.Contains(t, decision.MatchedRules, RequestRiskRuleHighChineseIntensity)
	require.Equal(t, "Asia/Shanghai", repo.events[0].Timezone)
}

func TestRequestRiskObserveModeRecordsOnly(t *testing.T) {
	repo := &requestRiskTestRepo{}
	svc := requestRiskTestService(repo)
	_, err := svc.UpdateRequestRiskControlConfig(context.Background(), UpdateRequestRiskControlConfigInput{
		Mode: requestRiskPtrString(RequestRiskControlModeObserve),
	})
	require.NoError(t, err)
	decision, err := svc.EvaluateRequestRisk(context.Background(), RequestRiskEvaluationInput{
		APIKeyID: 20,
		Headers: http.Header{
			"User-Agent":    []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
			"Inference-Geo": []string{"CN"},
		},
		Body: []byte(`{"input":[{"content":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.False(t, decision.Blocked)
	require.Len(t, repo.events, 1)
	require.Equal(t, RequestRiskActionBanUserAgent, repo.events[0].Action)
	require.Empty(t, repo.uaBans)
}

func TestRequestRiskUserAgentBanScopedByAPIKey(t *testing.T) {
	repo := &requestRiskTestRepo{}
	svc := requestRiskTestService(repo)
	headers := http.Header{
		"User-Agent":    []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
		"Inference-Geo": []string{"CN"},
	}
	decision, err := svc.EvaluateRequestRisk(context.Background(), RequestRiskEvaluationInput{
		UserID:   10,
		APIKeyID: 20,
		Headers:  headers,
		Body:     []byte(`{"input":[{"content":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, RequestRiskActionBanUserAgent, decision.Action)

	decision, err = svc.EvaluateRequestRisk(context.Background(), RequestRiskEvaluationInput{
		UserID:   11,
		APIKeyID: 21,
		Headers: http.Header{
			"User-Agent": []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
		},
		Body: []byte(`{"input":[{"content":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.False(t, decision.Blocked)

	decision, err = svc.EvaluateRequestRisk(context.Background(), RequestRiskEvaluationInput{
		UserID:   10,
		APIKeyID: 20,
		Headers: http.Header{
			"User-Agent": []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"},
		},
		Body: []byte(`{"input":[{"content":[{"text":"hello"}]}]}`),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Contains(t, decision.MatchedRules, RequestRiskRuleExistingUserAgentBan)
}

func requestRiskPtrString(value string) *string {
	return &value
}
