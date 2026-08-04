//go:build unit

package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

func TestFeishuEventSignatureUsesOpaqueHeaders(t *testing.T) {
	body := []byte(`{"schema":"2.0"}`)
	headers := FeishuEventHeaders{Timestamp: "opaque-timestamp", Nonce: "nonce"}
	sum := sha256.Sum256(append([]byte(headers.Timestamp+headers.Nonce+"encrypt-key"), body...))
	headers.Signature = hex.EncodeToString(sum[:])
	require.NoError(t, verifyFeishuEventSignature(headers, "encrypt-key", body))
	headers.Signature = "invalid"
	require.ErrorIs(t, verifyFeishuEventSignature(headers, "encrypt-key", body), ErrFeishuEventUnauthorized)
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

func TestFeishuEventEncryptedURLVerificationAllowsTokenHandshakeWithoutSignature(t *testing.T) {
	svc, events := newFeishuEventTestService()
	repo := svc.settingRepo.(*contentModerationTestSettingRepo)
	const encryptKey = "configured-encrypt-key"
	repo.values[SettingKeyFeishuNotifyEncryptKey] = encryptKey

	plain := []byte(`{"type":"url_verification","token":"verify-token","challenge":"encrypted-challenge"}`)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	require.NoError(t, err)
	iv := bytes.Repeat([]byte{0x42}, aes.BlockSize)
	ciphertext := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plain)
	encoded := make([]byte, 0, len(iv)+len(ciphertext))
	encoded = append(encoded, iv...)
	encoded = append(encoded, ciphertext...)
	body, err := json.Marshal(map[string]string{"encrypt": base64.StdEncoding.EncodeToString(encoded)})
	require.NoError(t, err)

	result, err := svc.VerifyAndReceiveEvent(t.Context(), FeishuEventHeaders{}, body)
	require.NoError(t, err)
	require.Equal(t, "encrypted-challenge", result.Challenge)
	require.Zero(t, events.received)
}
