package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreConformance(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	key := mustParseKey(t, "file/720p/out0.ts")
	expectedSize := int64(len("segment-data"))
	info, err := store.Put(ctx, key, strings.NewReader("segment-data"), PutOptions{ExpectedSize: &expectedSize})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if info.Size != expectedSize {
		t.Fatalf("Put() size = %d, want %d", info.Size, expectedSize)
	}

	object, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := object.Body.Seek(8, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	data, err := io.ReadAll(object.Body)
	object.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "data" {
		t.Fatalf("read data = %q, want %q", data, "data")
	}

	prefix := mustParseKey(t, "file")
	var walked []string
	if err := store.Walk(ctx, prefix, func(info ObjectInfo) error {
		walked = append(walked, info.Key.String())
		return nil
	}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if len(walked) != 1 || walked[0] != key.String() {
		t.Fatalf("Walk() = %v, want [%s]", walked, key.String())
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := store.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() after delete error = %v, want ErrNotFound", err)
	}
}

func TestLocalStoreRejectsSizeMismatchWithoutPublishing(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	key := mustParseKey(t, "file/object")
	expected := int64(100)
	if _, err := store.Put(context.Background(), key, strings.NewReader("short"), PutOptions{ExpectedSize: &expected}); err == nil {
		t.Fatal("Put() error = nil, want size mismatch")
	}
	if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat() error = %v, want ErrNotFound", err)
	}
}

func TestDeletePrefixRemovesObjectsAndEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	ctx := context.Background()
	for _, value := range []string{"file/720p/out.m3u8", "file/720p/out0.ts", "file/audio/audio0.ts"} {
		if _, err := store.Put(ctx, mustParseKey(t, value), strings.NewReader(value), PutOptions{}); err != nil {
			t.Fatalf("Put(%q) error = %v", value, err)
		}
	}
	if err := DeletePrefix(ctx, store, mustParseKey(t, "file")); err != nil {
		t.Fatalf("DeletePrefix() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "file")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("media directory still exists or stat failed unexpectedly: %v", err)
	}
	if err := DeletePrefix(ctx, store, mustParseKey(t, "missing")); err != nil {
		t.Fatalf("DeletePrefix() for missing prefix error = %v", err)
	}
}

func mustParseKey(t *testing.T, value string) Key {
	t.Helper()
	key, err := ParseKey(value)
	if err != nil {
		t.Fatalf("ParseKey(%q) error = %v", value, err)
	}
	return key
}
