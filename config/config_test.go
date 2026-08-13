package config

import "testing"

func TestLoadEnvStorageEncryptionKey(t *testing.T) {
	t.Setenv("StorageEncryptionKey", "base64-key")
	if got := LoadEnv().StorageEncryptionKey; got != "base64-key" {
		t.Fatalf("StorageEncryptionKey = %q", got)
	}
}
