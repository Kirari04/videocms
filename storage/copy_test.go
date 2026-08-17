package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type openCountingStore struct {
	Store
	opens int
}

func (s *openCountingStore) Open(ctx context.Context, key Key) (*Object, error) {
	s.opens++
	return s.Store.Open(ctx, key)
}

func TestCopyObjectVerifiedCopiesMetadataAndSkipsVerifiedDestination(t *testing.T) {
	source, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := copyTestKey(t, "video/source/original.mp4")
	size := int64(len("verified media"))
	if _, err := source.Put(context.Background(), key, strings.NewReader("verified media"), PutOptions{ContentType: "video/mp4", CacheControl: "private", ExpectedSize: &size}); err != nil {
		t.Fatal(err)
	}
	info, err := source.Stat(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	first, err := CopyObjectVerified(context.Background(), source, destination, info)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Copied || first.Checksum == "" || first.Info.Size != size {
		t.Fatalf("unexpected first copy: %#v", first)
	}
	second, err := CopyObjectVerified(context.Background(), source, destination, info)
	if err != nil {
		t.Fatal(err)
	}
	if second.Copied || second.Checksum != first.Checksum {
		t.Fatalf("verified destination was not reused: %#v", second)
	}
}

func TestCopyObjectValidatedUsesTransportValidationWithoutDestinationReadback(t *testing.T) {
	source, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destinationLocal, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination := &openCountingStore{Store: destinationLocal}
	key := copyTestKey(t, "video/1080p/out288.ts")
	size := int64(len("segment-data"))
	if _, err := source.Put(context.Background(), key, strings.NewReader("segment-data"), PutOptions{ExpectedSize: &size}); err != nil {
		t.Fatal(err)
	}
	info, err := source.Stat(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CopyObjectValidated(context.Background(), source, destination, info)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Copied || result.Checksum == "" || result.Info.Size != size {
		t.Fatalf("unexpected validated copy: %#v", result)
	}
	if destination.opens != 0 {
		t.Fatalf("destination was downloaded %d time(s) after upload", destination.opens)
	}
	object, err := destinationLocal.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	if err != nil || string(data) != "segment-data" {
		t.Fatalf("stored data = %q, %v", data, err)
	}
}

func TestPrefixInventoryOrdersManifestsLast(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"video/stream/index.m3u8", "video/stream/segment.ts", "video/stream/audio.m3u8"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	objects, err := PrefixInventory(context.Background(), store, copyTestKey(t, "video"))
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 3 || strings.HasSuffix(objects[0].Key.String(), ".m3u8") || !strings.HasSuffix(objects[2].Key.String(), ".m3u8") {
		t.Fatalf("unexpected inventory order: %#v", objects)
	}
}

func copyTestKey(t *testing.T, value string) Key {
	t.Helper()
	key, err := ParseKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
