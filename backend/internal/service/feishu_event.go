package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrFeishuEventUnauthorized = errors.New("feishu event verification failed")
	ErrFeishuEventInvalid      = errors.New("invalid feishu event")
)

type FeishuEventHeaders struct {
	Timestamp string
	Nonce     string
	Signature string
}

type FeishuEventAcceptResult struct {
	Challenge  string
	CardAction bool
	Duplicate  bool
}

type feishuEventEnvelope struct {
	Schema    string `json:"schema"`
	Token     string `json:"token"`
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Encrypt   string `json:"encrypt"`
	OpenID    string `json:"open_id"`
	TenantKey string `json:"tenant_key"`
	Action    struct {
		Value json.RawMessage `json:"value"`
	} `json:"action"`
	Header struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		TenantKey string `json:"tenant_key"`
		AppID     string `json:"app_id"`
		Token     string `json:"token"`
	} `json:"header"`
	Event struct {
		Sender struct {
			SenderID struct {
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
		} `json:"sender"`
		Operator struct {
			OpenID     string `json:"open_id"`
			OperatorID struct {
				OpenID string `json:"open_id"`
			} `json:"operator_id"`
		} `json:"operator"`
		OperatorID struct {
			OpenID string `json:"open_id"`
		} `json:"operator_id"`
	} `json:"event"`
}

func (s *FeishuNotificationService) VerifyAndReceiveEvent(ctx context.Context, headers FeishuEventHeaders, body []byte) (FeishuEventAcceptResult, error) {
	if s == nil {
		return FeishuEventAcceptResult{}, fmt.Errorf("feishu notification service is unavailable")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil || !cfg.Enabled || cfg.AppID == "" || cfg.VerificationToken == "" {
		return FeishuEventAcceptResult{}, fmt.Errorf("%w: integration configuration", ErrFeishuEventUnauthorized)
	}
	if len(body) == 0 || len(body) > 1<<20 {
		return FeishuEventAcceptResult{}, ErrFeishuEventInvalid
	}

	var encrypted feishuEventEnvelope
	if err := json.Unmarshal(body, &encrypted); err != nil {
		return FeishuEventAcceptResult{}, ErrFeishuEventInvalid
	}
	plainBody := body
	event := encrypted
	if encrypted.Encrypt != "" {
		if cfg.EncryptKey == "" {
			return FeishuEventAcceptResult{}, fmt.Errorf("%w: encrypted payload without key", ErrFeishuEventUnauthorized)
		}
		plainBody, err = decryptFeishuEvent(encrypted.Encrypt, cfg.EncryptKey)
		if err != nil {
			return FeishuEventAcceptResult{}, fmt.Errorf("%w: payload decryption", ErrFeishuEventUnauthorized)
		}
		if err := json.Unmarshal(plainBody, &event); err != nil {
			return FeishuEventAcceptResult{}, ErrFeishuEventInvalid
		}
	}

	token := strings.TrimSpace(event.Header.Token)
	if token == "" {
		token = strings.TrimSpace(event.Token)
	}
	// Feishu URL verification can be delivered either as plain JSON or inside
	// an encrypted envelope before signature headers are available. The
	// verification token (and, for encrypted payloads, successful decryption)
	// authenticates this one-shot handshake. Normal events remain signed.
	if event.Challenge != "" || event.Type == "url_verification" {
		if event.Challenge == "" || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.VerificationToken)) != 1 {
			return FeishuEventAcceptResult{}, fmt.Errorf("%w: challenge token", ErrFeishuEventUnauthorized)
		}
		return FeishuEventAcceptResult{Challenge: event.Challenge}, nil
	}
	if s.eventRepo == nil {
		return FeishuEventAcceptResult{}, fmt.Errorf("feishu event repository is unavailable")
	}
	legacyCardAction := event.Header.EventType == "" && (event.Type == "card.action.trigger" || (event.OpenID != "" && len(event.Action.Value) > 0))
	cardAction := legacyCardAction || event.Header.EventType == "card.action.trigger"
	cardSignaturePresent := strings.TrimSpace(headers.Signature) != ""
	if cardAction && cardSignaturePresent {
		if err := verifyFeishuCardSignature(headers, cfg.VerificationToken, body, time.Now()); err != nil {
			return FeishuEventAcceptResult{}, err
		}
	} else if !cardAction && cfg.EncryptKey != "" {
		if err := verifyFeishuEventSignature(headers, cfg.EncryptKey, body, time.Now()); err != nil {
			return FeishuEventAcceptResult{}, err
		}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.VerificationToken)) != 1 {
		return FeishuEventAcceptResult{}, fmt.Errorf("%w: payload token", ErrFeishuEventUnauthorized)
	}
	if event.Header.AppID != "" && event.Header.AppID != cfg.AppID {
		return FeishuEventAcceptResult{}, fmt.Errorf("%w: app id", ErrFeishuEventUnauthorized)
	}
	eventID := strings.TrimSpace(event.Header.EventID)
	eventType := strings.TrimSpace(event.Header.EventType)
	if legacyCardAction {
		eventType = "card.action.trigger_v1"
		idHash := sha256.Sum256(append([]byte(headers.Timestamp+headers.Nonce), body...))
		eventID = "legacy-card-" + hex.EncodeToString(idHash[:])
	}
	if eventID == "" || eventType == "" {
		return FeishuEventAcceptResult{}, ErrFeishuEventInvalid
	}
	hash := sha256.Sum256(plainBody)
	senderOpenID := firstNonEmpty(event.Event.Sender.SenderID.OpenID, event.Event.Operator.OpenID, event.Event.Operator.OperatorID.OpenID, event.Event.OperatorID.OpenID, event.OpenID)
	tenantKey := firstNonEmpty(event.Header.TenantKey, event.TenantKey)
	_, inserted, err := s.eventRepo.Receive(ctx, FeishuEventReceiptInput{
		AppID: cfg.AppID, EventID: eventID, EventType: eventType,
		TenantKey: tenantKey, SenderOpenID: senderOpenID,
		Payload: append(json.RawMessage(nil), plainBody...), PayloadSHA256: hex.EncodeToString(hash[:]),
	})
	if err != nil {
		return FeishuEventAcceptResult{}, err
	}
	return FeishuEventAcceptResult{Duplicate: !inserted, CardAction: cardAction}, nil
}

func verifyFeishuCardSignature(headers FeishuEventHeaders, verificationToken string, body []byte, now time.Time) error {
	timestamp, err := strconv.ParseInt(strings.TrimSpace(headers.Timestamp), 10, 64)
	if err != nil || strings.TrimSpace(headers.Nonce) == "" || strings.TrimSpace(headers.Signature) == "" || strings.TrimSpace(verificationToken) == "" {
		return fmt.Errorf("%w: missing card signature headers", ErrFeishuEventUnauthorized)
	}
	if delta := now.Sub(time.Unix(timestamp, 0)); delta > 5*time.Minute || delta < -5*time.Minute {
		return fmt.Errorf("%w: stale card signature", ErrFeishuEventUnauthorized)
	}
	sum := sha1.Sum(append([]byte(headers.Timestamp+headers.Nonce+verificationToken), body...))
	expected := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(headers.Signature)), []byte(expected)) != 1 {
		return fmt.Errorf("%w: card signature mismatch", ErrFeishuEventUnauthorized)
	}
	return nil
}

func verifyFeishuEventSignature(headers FeishuEventHeaders, encryptKey string, body []byte, now time.Time) error {
	timestamp, err := strconv.ParseInt(strings.TrimSpace(headers.Timestamp), 10, 64)
	if err != nil || strings.TrimSpace(headers.Nonce) == "" || strings.TrimSpace(headers.Signature) == "" {
		return fmt.Errorf("%w: missing event signature headers", ErrFeishuEventUnauthorized)
	}
	if delta := now.Sub(time.Unix(timestamp, 0)); delta > 5*time.Minute || delta < -5*time.Minute {
		return fmt.Errorf("%w: stale event signature", ErrFeishuEventUnauthorized)
	}
	sum := sha256.Sum256(append([]byte(headers.Timestamp+headers.Nonce+encryptKey), body...))
	expected := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(headers.Signature)), []byte(expected)) != 1 {
		return fmt.Errorf("%w: event signature mismatch", ErrFeishuEventUnauthorized)
	}
	return nil
}

func decryptFeishuEvent(value, encryptKey string) ([]byte, error) {
	encoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(encoded) < 2*aes.BlockSize {
		return nil, ErrFeishuEventInvalid
	}
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrFeishuEventInvalid
	}
	iv, ciphertext := encoded[:aes.BlockSize], encoded[aes.BlockSize:]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, ErrFeishuEventInvalid
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	start, end := bytes.IndexByte(plain, '{'), bytes.LastIndexByte(plain, '}')
	if start < 0 || end < start {
		return nil, ErrFeishuEventInvalid
	}
	return plain[start : end+1], nil
}
