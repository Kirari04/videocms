package config

import "testing"

func TestLoadEnvStorageDefaults(t *testing.T) {
	for _, key := range storageEnvironmentKeys() {
		t.Setenv(key, "")
	}

	got := LoadEnv()
	if got.StorageDefaultStore != "local" {
		t.Fatalf("StorageDefaultStore = %q, want local", got.StorageDefaultStore)
	}
	if got.StorageS3Region != "us-east-1" {
		t.Fatalf("StorageS3Region = %q, want us-east-1", got.StorageS3Region)
	}
	if got.StorageS3UsePathStyle == nil || *got.StorageS3UsePathStyle {
		t.Fatalf("StorageS3UsePathStyle = %v, want false", got.StorageS3UsePathStyle)
	}
	if got.StorageS3UploadPartSize != 16*1024*1024 {
		t.Fatalf("StorageS3UploadPartSize = %d, want 16 MiB", got.StorageS3UploadPartSize)
	}
	if got.StorageS3UploadConcurrency != 4 {
		t.Fatalf("StorageS3UploadConcurrency = %d, want 4", got.StorageS3UploadConcurrency)
	}
}

func TestLoadEnvStorageS3Configuration(t *testing.T) {
	values := map[string]string{
		"StorageDefaultStore":        "s3",
		"StorageS3Bucket":            "media",
		"StorageS3Region":            "eu-central-1",
		"StorageS3Endpoint":          "https://objects.example.com",
		"StorageS3Prefix":            "videocms/production",
		"StorageS3AccessKeyID":       "access-key",
		"StorageS3SecretAccessKey":   "secret-key",
		"StorageS3SessionToken":      "session-token",
		"StorageS3UsePathStyle":      "true",
		"StorageS3UploadPartSize":    "8388608",
		"StorageS3UploadConcurrency": "2",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}

	got := LoadEnv()
	if got.StorageDefaultStore != "s3" || got.StorageS3Bucket != "media" || got.StorageS3Region != "eu-central-1" {
		t.Fatalf("storage selection was not loaded: %#v", got)
	}
	if got.StorageS3Endpoint != values["StorageS3Endpoint"] || got.StorageS3Prefix != values["StorageS3Prefix"] {
		t.Fatalf("S3 location was not loaded: %#v", got)
	}
	if got.StorageS3AccessKeyID != "access-key" || got.StorageS3SecretAccessKey != "secret-key" || got.StorageS3SessionToken != "session-token" {
		t.Fatal("S3 credentials were not loaded")
	}
	if got.StorageS3UsePathStyle == nil || !*got.StorageS3UsePathStyle {
		t.Fatalf("StorageS3UsePathStyle = %v, want true", got.StorageS3UsePathStyle)
	}
	if got.StorageS3UploadPartSize != 8*1024*1024 || got.StorageS3UploadConcurrency != 2 {
		t.Fatalf("S3 upload settings = %d/%d, want 8 MiB/2", got.StorageS3UploadPartSize, got.StorageS3UploadConcurrency)
	}
}

func storageEnvironmentKeys() []string {
	return []string{
		"StorageDefaultStore",
		"StorageS3Bucket",
		"StorageS3Region",
		"StorageS3Endpoint",
		"StorageS3Prefix",
		"StorageS3AccessKeyID",
		"StorageS3SecretAccessKey",
		"StorageS3SessionToken",
		"StorageS3UsePathStyle",
		"StorageS3UploadPartSize",
		"StorageS3UploadConcurrency",
	}
}
