package storage

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestCredentialCipherRoundTripAndAuthentication(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := NewCredentialCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt([]byte(`{"secret":"value"}`), "mount-one")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, encryptedSecretVersion) || strings.Contains(encrypted, "value") {
		t.Fatalf("encrypted value = %q", encrypted)
	}
	decrypted, err := cipher.Decrypt(encrypted, "mount-one")
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != `{"secret":"value"}` {
		t.Fatalf("decrypted value = %q", decrypted)
	}
	if _, err := cipher.Decrypt(encrypted, "mount-two"); err == nil {
		t.Fatal("Decrypt() with a different mount ID succeeded")
	}
}

func TestCredentialCipherRequiresValidKey(t *testing.T) {
	if _, err := NewCredentialCipher(""); !errors.Is(err, ErrEncryptionKeyNotConfigured) {
		t.Fatalf("empty key error = %v", err)
	}
	if _, err := NewCredentialCipher("not-base64"); err == nil {
		t.Fatal("invalid base64 key error = nil")
	}
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := NewCredentialCipher(short); err == nil {
		t.Fatal("short key error = nil")
	}
}
