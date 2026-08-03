//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type feishuEventTestRepo struct {
	input    FeishuEventReceiptInput
	received int
}

func (r *feishuEventTestRepo) Receive(_ context.Context, input FeishuEventReceiptInput) (int64, bool, error) {
	r.input = input
	r.received++
	return 1, true, nil
}
func (r *feishuEventTestRepo) Claim(context.Context, string, int, time.Duration) ([]FeishuEventReceipt, error) {
	return nil, nil
}
func (r *feishuEventTestRepo) Complete(context.Context, int64, string, string) error { return nil }
func (r *feishuEventTestRepo) Retry(context.Context, int64, string, time.Time, string) error {
	return nil
}

func newFeishuEventTestService() (*FeishuNotificationService, *feishuEventTestRepo) {
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyFeishuNotifyEnabled:           "true",
		SettingKeyFeishuNotifyAppID:             "cli-test",
		SettingKeyFeishuNotifyAppSecret:         "secret",
		SettingKeyFeishuNotifyVerificationToken: "verify-token",
	}}
	events := &feishuEventTestRepo{}
	svc := NewFeishuNotificationService(settings, &feishuNotificationTestBindingRepo{})
	svc.eventRepo = events
	return svc, events
}

func TestFeishuEventSignatureRejectsReplay(t *testing.T) {
	body := []byte(`{"schema":"2.0"}`)
	now := time.Now().Truncate(time.Second)
	headers := FeishuEventHeaders{Timestamp: fmt.Sprint(now.Unix()), Nonce: "nonce"}
	sum := sha256.Sum256(append([]byte(headers.Timestamp+headers.Nonce+"encrypt-key"), body...))
	headers.Signature = hex.EncodeToString(sum[:])
	require.NoError(t, verifyFeishuEventSignature(headers, "encrypt-key", body, now))
	require.ErrorIs(t, verifyFeishuEventSignature(headers, "encrypt-key", body, now.Add(6*time.Minute)), ErrFeishuEventUnauthorized)
}

func TestFeishuEventURLVerificationChecksToken(t *testing.T) {
	svc, events := newFeishuEventTestService()
	body := []byte(`{"type":"url_verification","token":"verify-token","challenge":"challenge-value"}`)
	result, err := svc.VerifyAndReceiveEvent(context.Background(), FeishuEventHeaders{}, body)
	require.NoError(t, err)
	require.Equal(t, "challenge-value", result.Challenge)
	require.Zero(t, events.received)
}

func TestFeishuEventMessageIsPersistedForAsyncProcessing(t *testing.T) {
	svc, events := newFeishuEventTestService()
	body, err := json.Marshal(map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"event_id": "evt-1", "event_type": "im.message.receive_v1", "tenant_key": "tenant",
			"app_id": "cli-test", "token": "verify-token",
		},
		"event": map[string]any{"sender": map[string]any{"sender_id": map[string]any{"open_id": "ou-1"}}},
	})
	require.NoError(t, err)
	result, err := svc.VerifyAndReceiveEvent(context.Background(), FeishuEventHeaders{}, body)
	require.NoError(t, err)
	require.False(t, result.Duplicate)
	require.Equal(t, "evt-1", events.input.EventID)
	require.Equal(t, "ou-1", events.input.SenderOpenID)
}

func TestFeishuEventRejectsWrongVerificationToken(t *testing.T) {
	svc, _ := newFeishuEventTestService()
	_, err := svc.VerifyAndReceiveEvent(context.Background(), FeishuEventHeaders{}, []byte(`{"type":"url_verification","token":"wrong","challenge":"x"}`))
	require.ErrorIs(t, err, ErrFeishuEventUnauthorized)
}

func TestFeishuEventURLVerificationAllowsTokenHandshakeWhenEncryptKeyConfigured(t *testing.T) {
	svc, events := newFeishuEventTestService()
	repo := svc.settingRepo.(*contentModerationTestSettingRepo)
	repo.values[SettingKeyFeishuNotifyEncryptKey] = "configured-encrypt-key"

	result, err := svc.VerifyAndReceiveEvent(context.Background(), FeishuEventHeaders{}, []byte(`{"type":"url_verification","token":"verify-token","challenge":"challenge-without-signature"}`))
	require.NoError(t, err)
	require.Equal(t, "challenge-without-signature", result.Challenge)
	require.Zero(t, events.received)
}
