package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type opaqueStore struct{ Store }

func TestPublishAndMaterializePrefix(t *testing.T) {
	ctx := context.Background()
	storeRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	sourceRoot := t.TempDir()
	local, err := NewLocalStore(storeRoot)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewLocalWorkspace(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithWorkspace("remote", LegacyMediaLayout{}, workspace, map[string]Store{
		"remote": opaqueStore{Store: local},
	})
	if err != nil {
		t.Fatal(err)
	}

	mustWriteStorageTestFile(t, filepath.Join(sourceRoot, "out0.ts"), "segment")
	mustWriteStorageTestFile(t, filepath.Join(sourceRoot, "stream.m3u8"), "manifest")
	prefix, err := JoinKey("file", "720p")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := JoinKey("file", "720p", "stale.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Put(ctx, stale, strings.NewReader("stale"), PutOptions{}); err != nil {
		t.Fatal(err)
	}

	published, err := service.PublishPrefix(ctx, "remote", prefix, sourceRoot, PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(published) != 2 {
		t.Fatalf("published %d objects, want 2", len(published))
	}
	if _, err := local.Stat(ctx, stale); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale object was not removed: %v", err)
	}

	materialized, cleanup, err := service.MaterializePrefix(ctx, "remote", prefix, "download-input")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(filepath.Join(materialized, "out0.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "segment" {
		t.Fatalf("materialized segment = %q", data)
	}
}

func TestMaterializeUsesLocalPathWithoutRemovingIt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local, err := NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", LegacyMediaLayout{}, map[string]Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	key, err := JoinKey("file", "source", "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Put(ctx, key, strings.NewReader("source"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := service.Materialize(ctx, "local", key, "encoder-input", ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "source" {
		t.Fatalf("local materialization was removed or changed: %q, %v", data, err)
	}
}

func mustWriteStorageTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
