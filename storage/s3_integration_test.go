package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestS3StoreIntegration(t *testing.T) {
	endpoint := os.Getenv("VIDEOCMS_S3_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Skip("VIDEOCMS_S3_INTEGRATION_ENDPOINT is not configured")
	}
	bucket := os.Getenv("VIDEOCMS_S3_INTEGRATION_BUCKET")
	if bucket == "" {
		t.Fatal("VIDEOCMS_S3_INTEGRATION_BUCKET is required")
	}
	region := os.Getenv("VIDEOCMS_S3_INTEGRATION_REGION")
	if region == "" {
		region = defaultS3Region
	}
	store, err := NewS3Store(context.Background(), S3Options{
		Bucket:            bucket,
		Region:            region,
		Endpoint:          endpoint,
		Prefix:            "videocms-integration/" + uuid.NewString(),
		AccessKeyID:       os.Getenv("VIDEOCMS_S3_INTEGRATION_ACCESS_KEY_ID"),
		SecretAccessKey:   os.Getenv("VIDEOCMS_S3_INTEGRATION_SECRET_ACCESS_KEY"),
		UsePathStyle:      true,
		UploadPartSize:    minimumS3UploadPartSize,
		UploadConcurrency: 2,
	})
	if err != nil {
		t.Fatalf("NewS3Store() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	largeData := bytes.Repeat([]byte("0123456789abcdef"), int(minimumS3UploadPartSize/16)+1024)
	key := mustParseKey(t, "file/720p/out0.ts")
	expectedSize := int64(len(largeData))
	info, err := store.Put(ctx, key, bytes.NewReader(largeData), PutOptions{
		ExpectedSize: &expectedSize,
		ContentType:  "video/mp2t",
		CacheControl: "public, max-age=60",
	})
	if err != nil {
		t.Fatalf("multipart Put() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })
	if info.Size != expectedSize || info.ContentType != "video/mp2t" || info.CacheControl != "public, max-age=60" {
		t.Fatalf("stored info = %#v", info)
	}

	object, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer object.Body.Close()
	offset := minimumS3UploadPartSize - 8
	if _, err := object.Body.Seek(offset, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	got := make([]byte, 32)
	if _, err := io.ReadFull(object.Body, got); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if !bytes.Equal(got, largeData[offset:offset+int64(len(got))]) {
		t.Fatal("range-backed read returned incorrect data")
	}

	secondKey := mustParseKey(t, "file/audio/audio0.ts")
	if _, err := store.Put(ctx, secondKey, bytes.NewReader([]byte("audio")), PutOptions{}); err != nil {
		t.Fatalf("second Put() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), secondKey) })
	outsideKey := mustParseKey(t, "file-other/out0.ts")
	if _, err := store.Put(ctx, outsideKey, bytes.NewReader([]byte("outside")), PutOptions{}); err != nil {
		t.Fatalf("outside Put() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), outsideKey) })

	var walked []string
	if err := store.Walk(ctx, mustParseKey(t, "file"), func(info ObjectInfo) error {
		walked = append(walked, info.Key.String())
		return nil
	}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	slices.Sort(walked)
	want := []string{secondKey.String(), key.String()}
	slices.Sort(want)
	if !slices.Equal(walked, want) {
		t.Fatalf("Walk() = %v, want %v", walked, want)
	}

	if err := store.Delete(ctx, secondKey); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(ctx, secondKey); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
}
