package storage

import (
	"encoding/base64"
	"encoding/json"
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

func TestEncodeSFTPMountSeparatesAndEncryptsCredentials(t *testing.T) {
	cipher, err := NewCredentialCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	configuration, encryptedCredentials, err := EncodeSFTPMount(
		SFTPMountConfiguration{
			Host: " storage.example.com ", Username: "media", Root: " uploads/../media ",
			Authentication: SFTPAuthenticationPassword,
			HostKeyFingerprints: []string{
				" SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA ",
				"SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			},
		},
		SFTPMountCredentials{Password: "secret-value"},
		"mount-sftp",
		cipher,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(configuration, "secret-value") || strings.Contains(encryptedCredentials, "secret-value") {
		t.Fatal("SFTP password was stored in plaintext")
	}
	decodedConfiguration, err := DecodeSFTPMountConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if decodedConfiguration.Host != "storage.example.com" || decodedConfiguration.Port != 22 || decodedConfiguration.Root != "media" || len(decodedConfiguration.HostKeyFingerprints) != 1 {
		t.Fatalf("normalized configuration = %#v", decodedConfiguration)
	}
	decodedCredentials, err := DecodeSFTPMountCredentials(encryptedCredentials, "mount-sftp", cipher)
	if err != nil {
		t.Fatal(err)
	}
	if decodedCredentials.Password != "secret-value" {
		t.Fatalf("decoded credentials = %#v", decodedCredentials)
	}
}

func TestEncodeMountPreservesCredentialsAndRejectsUnknownFields(t *testing.T) {
	cipher, err := NewCredentialCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	configuration := json.RawMessage(`{
		"host":"storage.example.com","port":22,"username":"media","root":"media",
		"authentication":"password","host_key_fingerprints":["SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"]
	}`)
	credentials := json.RawMessage(`{"password":"secret-value"}`)
	encodedConfiguration, encryptedCredentials, err := EncodeMount(MountProviderSFTP, configuration, &credentials, "", "mount-sftp", cipher)
	if err != nil {
		t.Fatal(err)
	}
	updatedConfiguration := json.RawMessage(`{
		"host":"storage.example.com","port":22,"username":"media","root":"media",
		"authentication":"password","host_key_fingerprints":["SHA256:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"]
	}`)
	_, updatedEncryptedCredentials, err := EncodeMount(MountProviderSFTP, updatedConfiguration, nil, encryptedCredentials, "mount-sftp", cipher)
	if err != nil {
		t.Fatal(err)
	}
	decodedCredentials, err := DecodeSFTPMountCredentials(updatedEncryptedCredentials, "mount-sftp", cipher)
	if err != nil || decodedCredentials.Password != "secret-value" {
		t.Fatalf("preserved credentials = %#v, %v", decodedCredentials, err)
	}
	same, err := SameMountLocation(MountProviderSFTP, encodedConfiguration, string(updatedConfiguration))
	if err != nil || !same {
		t.Fatalf("SameMountLocation() = %t, %v", same, err)
	}
	invalidConfiguration := append(json.RawMessage(nil), configuration...)
	invalidConfiguration[len(invalidConfiguration)-1] = ','
	invalidConfiguration = append(invalidConfiguration, []byte(`"unexpected":true}`)...)
	if _, _, err := EncodeMount(MountProviderSFTP, invalidConfiguration, &credentials, "", "mount-sftp", cipher); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("EncodeMount() error = %v, want unknown field", err)
	}
}
