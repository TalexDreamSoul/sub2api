package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AESEncryptor implements SecretEncryptor using AES-256-GCM
type AESEncryptor struct {
	key atomic.Pointer[[32]byte]
}

// NewAESEncryptor creates a new AES encryptor
func NewAESEncryptor(cfg *config.Config) (service.SecretEncryptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("totp encryption config is required")
	}
	encryptor := &AESEncryptor{}
	if err := encryptor.ActivateKey(cfg.Totp.EncryptionKey); err != nil {
		return nil, err
	}
	return encryptor, nil
}

// ActivateKey atomically switches the active AES-256 key after validating it.
func (e *AESEncryptor) ActivateKey(keyHex string) error {
	decoded, err := hex.DecodeString(strings.TrimSpace(keyHex))
	if err != nil {
		return fmt.Errorf("invalid totp encryption key: %w", err)
	}
	if len(decoded) != 32 {
		return fmt.Errorf("totp encryption key must be 32 bytes (64 hex chars), got %d bytes", len(decoded))
	}
	var key [32]byte
	copy(key[:], decoded)
	e.key.Store(&key)
	return nil
}

func (e *AESEncryptor) activeKey() (*[32]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("totp encryptor is not configured")
	}
	key := e.key.Load()
	if key == nil {
		return nil, fmt.Errorf("totp encryption key is not active")
	}
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
// Output format: base64(nonce + ciphertext + tag)
func (e *AESEncryptor) Encrypt(plaintext string) (string, error) {
	key, err := e.activeKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt the plaintext
	// Seal appends the ciphertext and tag to the nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode as base64
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM
func (e *AESEncryptor) Decrypt(ciphertext string) (string, error) {
	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	key, err := e.activeKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertextData := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextData, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}
