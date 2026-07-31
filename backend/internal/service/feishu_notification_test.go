//go:build unit

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type feishuNotificationTestBindingRepo struct {
	binding *FeishuUserIdentityBinding
}

func (r *feishuNotificationTestBindingRepo) UpsertFeishuUserIdentityBinding(ctx context.Context, input UpsertFeishuUserIdentityBindingInput) (*FeishuUserIdentityBinding, error) {
	return nil, ErrFeishuNotificationConflict
}

func (r *feishuNotificationTestBindingRepo) GetFeishuNotificationBinding(ctx context.Context, userID int64, appID string) (*FeishuUserIdentityBinding, error) {
	if r == nil || r.binding == nil {
		return nil, ErrFeishuNotificationNotBound
	}
	return r.binding, nil
}

func (r *feishuNotificationTestBindingRepo) GetFeishuBindingByUnionID(ctx context.Context, appID, tenantKey, unionID, purpose string) (*FeishuUserIdentityBinding, error) {
	return nil, ErrFeishuNotificationNotBound
}

func (r *feishuNotificationTestBindingRepo) GetFeishuBindingByOpenID(ctx context.Context, appID, tenantKey, openID, purpose string) (*FeishuUserIdentityBinding, error) {
	if r == nil || r.binding == nil || r.binding.AppID != appID || r.binding.OpenID != openID {
		return nil, ErrFeishuNotificationNotBound
	}
	return r.binding, nil
}

func (r *feishuNotificationTestBindingRepo) ListFeishuBindingsByUser(ctx context.Context, userID int64) ([]FeishuUserIdentityBinding, error) {
	return nil, nil
}

func (r *feishuNotificationTestBindingRepo) ListFeishuChannelRecipientUserIDs(_ context.Context, _ string, afterUserID int64, _ int) ([]int64, error) {
	if r == nil || r.binding == nil || r.binding.UserID <= afterUserID || !r.binding.NotificationEnabled {
		return []int64{}, nil
	}
	return []int64{r.binding.UserID}, nil
}

func (r *feishuNotificationTestBindingRepo) SetFeishuNotificationEnabled(ctx context.Context, userID int64, appID string, enabled bool) (*FeishuUserIdentityBinding, error) {
	if r == nil || r.binding == nil || r.binding.UserID != userID || r.binding.AppID != appID {
		return nil, ErrFeishuNotificationNotBound
	}
	r.binding.NotificationEnabled = enabled
	return r.binding, nil
}

func (r *feishuNotificationTestBindingRepo) DeleteFeishuNotificationBinding(ctx context.Context, userID int64, appID string) error {
	return nil
}

func newFeishuNotificationTestService(t *testing.T, tokenBody any, messageHandler http.HandlerFunc) (*FeishuNotificationService, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if s, ok := tokenBody.(string); ok {
				_, _ = w.Write([]byte(s))
				return
			}
			require.NoError(t, json.NewEncoder(w).Encode(tokenBody))
		case "/messages":
			messageHandler(w, r)
		default:
			t.Fatalf("unexpected feishu path: %s", r.URL.Path)
		}
	}))
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyFeishuNotifyEnabled:           "true",
		SettingKeyFeishuNotifyAppID:             "cli-test",
		SettingKeyFeishuNotifyAppSecret:         "secret",
		SettingKeyFeishuNotifyTokenURL:          server.URL + "/token",
		SettingKeyFeishuNotifyMessageURL:        server.URL + "/messages",
		SettingKeyFeishuNotifyVerificationToken: "verification-token",
	}}
	bindingRepo := &feishuNotificationTestBindingRepo{binding: &FeishuUserIdentityBinding{
		UserID:              1,
		AppID:               "cli-test",
		OpenID:              "ou-test",
		NotificationEnabled: true,
	}}
	return NewFeishuNotificationService(settingRepo, bindingRepo), server.Close
}

func TestFeishuNotificationSendRejectsMalformedSuccessResponses(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		errContains string
	}{
		{name: "empty", body: "", errContains: "empty response body"},
		{name: "html", body: "<html>ok</html>", errContains: "invalid json response"},
		{name: "missing_code", body: `{"data":{"message_id":"om_x"}}`, errContains: "missing code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
			}, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})
			defer cleanup()

			err := svc.SendBalanceLow(context.Background(), FeishuBalanceLowNotification{UserID: 1, Balance: 1, Threshold: 2})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestFeishuNotificationSendAcceptsCodeZeroResponse(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
		"code":                0,
		"tenant_access_token": "tenant-token",
	}, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"message_id": "om_test"},
		})
	})
	defer cleanup()

	err := svc.SendBalanceLow(context.Background(), FeishuBalanceLowNotification{UserID: 1, Balance: 1, Threshold: 2})
	require.NoError(t, err)
}

func TestFeishuNotificationTokenRejectsMissingCodeResponse(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, `{"tenant_access_token":"tenant-token"}`, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
	})
	defer cleanup()

	err := svc.SendBalanceLow(context.Background(), FeishuBalanceLowNotification{UserID: 1, Balance: 1, Threshold: 2})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing code")
}

func TestFeishuTenantTokenIsCachedAcrossMessages(t *testing.T) {
	var tokenCalls atomic.Int64
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
		"code":                0,
		"tenant_access_token": "tenant-token",
		"expire":              7200,
	}, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"message_id": "om_test"},
		})
	})
	defer cleanup()
	svc.tokenFetchObserver = func() { tokenCalls.Add(1) }

	require.NoError(t, svc.SendTest(context.Background(), 1))
	require.NoError(t, svc.SendTest(context.Background(), 1))
	require.EqualValues(t, 1, tokenCalls.Load())
}

type feishuOutboxTestRepo struct {
	enqueued *FeishuNotificationOutboxInput
	items    []FeishuNotificationOutboxItem
	sentID   int64
	sentMsg  string
}

func (r *feishuOutboxTestRepo) Enqueue(_ context.Context, input FeishuNotificationOutboxInput) (int64, bool, error) {
	r.enqueued = &input
	return 9, true, nil
}
func (r *feishuOutboxTestRepo) Claim(context.Context, string, int, time.Duration) ([]FeishuNotificationOutboxItem, error) {
	items := r.items
	r.items = nil
	return items, nil
}
func (r *feishuOutboxTestRepo) MarkSent(_ context.Context, id int64, _ string, providerMessageID string) error {
	r.sentID = id
	r.sentMsg = providerMessageID
	return nil
}
func (r *feishuOutboxTestRepo) Retry(context.Context, int64, string, time.Time, string) error {
	return nil
}
func (r *feishuOutboxTestRepo) MarkDead(context.Context, int64, string, string) error { return nil }
func (r *feishuOutboxTestRepo) ListRecent(context.Context, int) ([]FeishuNotificationDelivery, error) {
	return nil, nil
}
func (r *feishuOutboxTestRepo) Stats(context.Context) (FeishuNotificationOutboxStats, error) {
	return FeishuNotificationOutboxStats{}, nil
}

func TestQueueFeishuAdminMessageUsesPlainTextOutbox(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
		"code":                0,
		"tenant_access_token": "tenant-token",
	}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox
	actorID := int64(3)

	id, inserted, err := svc.QueueAdminMessage(context.Background(), FeishuAdminMessageInput{
		UserID:         1,
		Content:        "额度已恢复，请重新尝试。",
		IdempotencyKey: "message-1",
		CreatedBy:      &actorID,
	})
	require.NoError(t, err)
	require.True(t, inserted)
	require.EqualValues(t, 9, id)
	require.NotNil(t, outbox.enqueued)
	require.Equal(t, FeishuNotificationCategoryAdminService, outbox.enqueued.Category)
	require.Contains(t, string(outbox.enqueued.Payload), `"tag":"plain_text"`)
	require.NotContains(t, string(outbox.enqueued.Payload), `"tag":"lark_md"`)
}

func TestAutomaticFeishuNotificationUsesReliableOutbox(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{"code": 0}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox
	err := svc.SendSubscriptionExpiryReminder(context.Background(), FeishuSubscriptionExpiryNotification{
		UserID: 1, SubscriptionID: 22, GroupName: "Pro", ExpiresAt: time.Now().Add(24 * time.Hour), DaysRemaining: 1, SourceReminderKey: "expiry-1d",
	})
	require.NoError(t, err)
	require.NotNil(t, outbox.enqueued)
	require.Equal(t, "subscription", outbox.enqueued.Category)
	require.Contains(t, outbox.enqueued.DedupeKey, "expiry-1d")
}

func TestFeishuOutboxWorkerRecordsProviderMessageID(t *testing.T) {
	var sentBody map[string]any
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
		"code":                0,
		"tenant_access_token": "tenant-token",
	}, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sentBody))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"message_id": "om_worker"},
		})
	})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{items: []FeishuNotificationOutboxItem{{
		ID:       12,
		UserID:   1,
		AppID:    "cli-test",
		Category: FeishuNotificationCategoryAdminService,
		Payload:  json.RawMessage(`{"config":{"wide_screen_mode":true},"elements":[]}`),
	}}}
	svc.outboxRepo = outbox
	svc.workerID = "worker-test"

	svc.processOutboxOnce(context.Background())
	require.EqualValues(t, 12, outbox.sentID)
	require.Equal(t, "om_worker", outbox.sentMsg)
	require.Equal(t, "sub2api-feishu-12", sentBody["uuid"])
}

type feishuPreferenceTestRepo struct {
	preferences map[string]bool
}

func (r *feishuPreferenceTestRepo) Get(_ context.Context, _ int64, _ string, categories []string) (map[string]bool, error) {
	result := make(map[string]bool, len(categories))
	for _, category := range categories {
		result[category] = true
	}
	for category, enabled := range r.preferences {
		result[category] = enabled
	}
	return result, nil
}

func (r *feishuPreferenceTestRepo) Set(_ context.Context, _ int64, _ string, preferences map[string]bool) error {
	r.preferences = preferences
	return nil
}

func TestFeishuOutboxWorkerSuppressesDisabledQueuedCategory(t *testing.T) {
	var messageCalls atomic.Int64
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
		"code":                0,
		"tenant_access_token": "tenant-token",
	}, func(http.ResponseWriter, *http.Request) {
		messageCalls.Add(1)
	})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{items: []FeishuNotificationOutboxItem{{
		ID:       13,
		UserID:   1,
		AppID:    "cli-test",
		Category: "quota",
		Payload:  json.RawMessage(`{"config":{"wide_screen_mode":true},"elements":[]}`),
	}}}
	svc.outboxRepo = outbox
	svc.preferenceRepo = &feishuPreferenceTestRepo{preferences: map[string]bool{"quota": false}}
	svc.workerID = "worker-test"

	svc.processOutboxOnce(context.Background())
	require.EqualValues(t, 13, outbox.sentID)
	require.Empty(t, outbox.sentMsg)
	require.Zero(t, messageCalls.Load())
}

func TestFeishuModerationNotificationUsesPersistedEventID(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{"code": 0}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox

	err := svc.SendContentModerationViolation(context.Background(), FeishuContentModerationViolationNotification{
		SourceEventID: 42,
		UserID:        1,
		Category:      "policy",
	})
	require.NoError(t, err)
	require.NotNil(t, outbox.enqueued)
	require.Contains(t, outbox.enqueued.DedupeKey, "moderation-violation:event:42")
}

func TestFeishuAPIKeyQuotaExhaustedUsesRequestDedupeAndMasksShortKey(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{"code": 0}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox

	err := svc.QueueAPIKeyQuotaExhausted(context.Background(), FeishuAPIKeyQuotaExhaustedNotification{
		UserID: 1, APIKeyID: 9, APIKeyName: "short-key", APIKeyValue: "abc", QuotaUSD: 10, SourceRequestID: "req-1",
	})
	require.NoError(t, err)
	require.NotNil(t, outbox.enqueued)
	require.Equal(t, "quota", outbox.enqueued.Category)
	require.Contains(t, outbox.enqueued.DedupeKey, "api-key-quota-exhausted:9:req-1")
	require.NotContains(t, string(outbox.enqueued.Payload), "abc")
}

func TestFeishuAPIKeyExpiryReminderIsDeduplicatedAndNeverExposesShortKey(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{"code": 0}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox
	expiresAt := time.Now().Add(36 * time.Hour)

	err := svc.queueAPIKeyExpiryReminder(context.Background(), &APIKey{
		ID: 9, UserID: 1, Name: "short-key", Key: "abc", ExpiresAt: &expiresAt,
	}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, outbox.enqueued)
	require.Equal(t, "quota", outbox.enqueued.Category)
	require.Contains(t, outbox.enqueued.DedupeKey, "api-key-expiry:9:")
	require.NotContains(t, string(outbox.enqueued.Payload), "abc")
	require.Contains(t, string(outbox.enqueued.Payload), "*******")
}

func TestFeishuCardActionAppliesConfirmedNotificationToggle(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{"code": 0}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox
	receipt := &FeishuEventReceipt{ID: 1, AppID: "cli-test", EventID: "evt-card", EventType: "card.action.trigger", SenderOpenID: "ou-test", Payload: json.RawMessage(`{"event":{"action":{"value":{"action":"notification_toggle","enabled":false}}}}`)}

	status, err := svc.queueFeishuEventReply(context.Background(), receipt)
	require.NoError(t, err)
	require.Equal(t, "processed", status)
	require.False(t, svc.bindingRepo.(*feishuNotificationTestBindingRepo).binding.NotificationEnabled)
	require.NotNil(t, outbox.enqueued)
	require.Contains(t, string(outbox.enqueued.Payload), "已关闭")
}

func TestFeishuDiagnosticsReportsConfigTokenBindingAndOutbox(t *testing.T) {
	svc, cleanup := newFeishuNotificationTestService(t, map[string]any{
		"code":                0,
		"tenant_access_token": "tenant-token",
		"expire":              7200,
	}, func(http.ResponseWriter, *http.Request) {})
	defer cleanup()
	outbox := &feishuOutboxTestRepo{}
	svc.outboxRepo = outbox

	report := svc.Diagnose(context.Background(), 1, false)
	require.True(t, report.Healthy)
	require.Len(t, report.Steps, 6)
	for _, step := range report.Steps {
		require.NotEqual(t, "failed", step.Status, step.Name)
	}
}

func TestBalanceLowEmailFallbackLogsWhenNoRecipients(t *testing.T) {
	var output strings.Builder
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	svc := &BalanceNotifyService{notificationEmailService: NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)}
	svc.sendBalanceLowEmails(nil, 42, "Alice", "alice@example.com", 1, 2, "Sub2API", "")

	require.Contains(t, output.String(), "balance low email fallback skipped")
	require.Contains(t, output.String(), "user_id=42")
}
