package storage

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncodeS3MountSeparatesAndEncryptsCredentials(t *testing.T) {
	cipher, err := NewCredentialCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	configuration, encryptedCredentials, err := EncodeS3Mount(
		S3MountConfiguration{
			Bucket:       " media ",
			Region:       "eu-central-1",
			Prefix:       "/videos/",
			UsePathStyle: true,
		},
		S3MountCredentials{
			AccessKeyID:     "access-key",
			SecretAccessKey: "secret-value",
		},
		"mount-a",
		cipher,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(configuration, "access-key") || strings.Contains(configuration, "secret-value") {
		t.Fatalf("configuration contains credentials: %s", configuration)
	}
	if strings.Contains(encryptedCredentials, "access-key") || strings.Contains(encryptedCredentials, "secret-value") {
		t.Fatal("encrypted credential value contains plaintext")
	}
	decodedConfiguration, err := DecodeS3MountConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if decodedConfiguration.Bucket != "media" || decodedConfiguration.Prefix != "videos" {
		t.Fatalf("normalized configuration = %#v", decodedConfiguration)
	}
	decodedCredentials, err := DecodeS3MountCredentials(encryptedCredentials, "mount-a", cipher)
	if err != nil {
		t.Fatal(err)
	}
	if decodedCredentials.AccessKeyID != "access-key" || decodedCredentials.SecretAccessKey != "secret-value" {
		t.Fatalf("decoded credentials = %#v", decodedCredentials)
	}
	if _, err := DecodeS3MountCredentials(encryptedCredentials, "mount-b", cipher); err == nil {
		t.Fatal("credentials decrypted under a different mount identity")
	}
}
