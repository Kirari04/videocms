package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrEncryptionKeyNotConfigured = errors.New("StorageEncryptionKey is not configured")

const encryptedSecretVersion = "v1:"

type CredentialCipher struct {
	aead cipher.AEAD
}

// NewCredentialCipher accepts a base64-encoded 32-byte AES-256 key. The key is
// installation configuration; adapter credentials encrypted with it remain in
// the database and are never returned by the admin API.
func NewCredentialCipher(encodedKey string) (*CredentialCipher, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return nil, ErrEncryptionKeyNotConfigured
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode StorageEncryptionKey: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("StorageEncryptionKey must decode to exactly 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize credential encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize credential encryption: %w", err)
	}
	return &CredentialCipher{aead: aead}, nil
}

func (c *CredentialCipher) Encrypt(plaintext []byte, mountID string) (string, error) {
	if c == nil || c.aead == nil {
		return "", ErrEncryptionKeyNotConfigured
	}
	if mountID == "" {
		return "", errors.New("storage mount ID is empty")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate credential nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, []byte(mountID))
	return encryptedSecretVersion + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *CredentialCipher) Decrypt(value string, mountID string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrEncryptionKeyNotConfigured
	}
	if !strings.HasPrefix(value, encryptedSecretVersion) {
		return nil, errors.New("unsupported encrypted credential version")
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedSecretVersion))
	if err != nil {
		return nil, fmt.Errorf("decode encrypted credentials: %w", err)
	}
	if len(sealed) < c.aead.NonceSize() {
		return nil, errors.New("encrypted credentials are truncated")
	}
	nonce, ciphertext := sealed[:c.aead.NonceSize()], sealed[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, []byte(mountID))
	if err != nil {
		return nil, errors.New("decrypt storage credentials: encryption key or mount identity does not match")
	}
	return plaintext, nil
}
