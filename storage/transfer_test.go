package storage

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type opaqueStore struct{ Store }

type wrongSizeStore struct{ Store }

func (s wrongSizeStore) Open(ctx context.Context, key Key) (*Object, error) {
	object, err := s.Store.Open(ctx, key)
	if err == nil {
		object.Info.Size++
	}
	return object, err
}

type failManifestStore struct{ Store }

func (s failManifestStore) Put(ctx context.Context, key Key, src io.Reader, opts PutOptions) (ObjectInfo, error) {
	if strings.HasSuffix(key.String(), ".m3u8") {
		return ObjectInfo{}, errors.New("injected manifest failure")
	}
	return s.Store.Put(ctx, key, src, opts)
}

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

func TestMaterializePrefixesReportsPartialObjectAndCombinedByteProgress(t *testing.T) {
	ctx := context.Background()
	backing, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewLocalWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithWorkspace("remote", LegacyMediaLayout{}, workspace, map[string]Store{
		"remote": opaqueStore{Store: backing},
	})
	if err != nil {
		t.Fatal(err)
	}
	objects := []struct {
		key  string
		data string
	}{
		{"file/video/nested/out0.ts", strings.Repeat("v", 1024*1024)},
		{"file/audio/audio0.ts", strings.Repeat("a", 512*1024)},
		{"file/subtitle/out.vtt", "text"},
	}
	var totalBytes int64
	for _, object := range objects {
		totalBytes += int64(len(object.data))
		if _, err := backing.Put(ctx, mustParseKey(t, object.key), strings.NewReader(object.data), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var updates []float64
	paths, cleanup, err := service.MaterializePrefixes(ctx, "remote", []PrefixMaterialization{
		{Prefix: mustParseKey(t, "file/video"), Purpose: "download-quality"},
		{Prefix: mustParseKey(t, "file/audio"), Purpose: "download-audio"},
		{Prefix: mustParseKey(t, "file/subtitle"), Purpose: "download-subtitle"},
	}, func(value float64) { updates = append(updates, value) })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(updates) < 3 || updates[0] != 0 || updates[len(updates)-1] != 1 {
		t.Fatalf("progress updates = %v, want partial progress between 0 and 1", updates)
	}
	// The first update must measure a partial video object against every track,
	// rather than wait for an entire object or treat each track as equally sized.
	wantFirst := float64(128*1024) / float64(totalBytes)
	if math.Abs(updates[1]-wantFirst) > 1e-9 {
		t.Fatalf("first byte progress = %v, want %v", updates[1], wantFirst)
	}
	for index := 1; index < len(updates); index++ {
		if updates[index] < updates[index-1] || updates[index] > 1 {
			t.Fatalf("progress is not monotonic: %v", updates)
		}
	}
	for index, relative := range []string{"nested/out0.ts", "audio0.ts", "out.vtt"} {
		data, err := os.ReadFile(filepath.Join(paths[index], relative))
		if err != nil || string(data) != objects[index].data {
			t.Fatalf("materialized %s differs: %v", relative, err)
		}
	}
}

func TestMaterializePrefixesCleansScratchOnFailureOrCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "size_mismatch", true: "canceled"}[canceled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			backing, err := NewLocalStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			workspaceRoot := t.TempDir()
			workspace, err := NewLocalWorkspace(workspaceRoot)
			if err != nil {
				t.Fatal(err)
			}
			store := Store(opaqueStore{Store: backing})
			if !canceled {
				store = wrongSizeStore{Store: backing}
			}
			service, err := NewServiceWithWorkspace("remote", LegacyMediaLayout{}, workspace, map[string]Store{"remote": store})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backing.Put(ctx, mustParseKey(t, "file/video/out0.ts"), strings.NewReader(strings.Repeat("v", 256*1024)), PutOptions{}); err != nil {
				t.Fatal(err)
			}
			completed := false
			_, _, err = service.MaterializePrefixes(ctx, "remote", []PrefixMaterialization{
				{Prefix: mustParseKey(t, "file/video"), Purpose: "download-quality"},
			}, func(value float64) {
				completed = completed || value == 1
				if canceled && value > 0 {
					cancel()
				}
			})
			if err == nil || completed {
				t.Fatalf("failed materialization = %v, completed = %v", err, completed)
			}
			if canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("materialization error = %v, want context.Canceled", err)
			}
			entries, err := os.ReadDir(workspaceRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("scratch after failure = %v, %v", entries, err)
			}
		})
	}
}

func TestMaterializePrefixesUsesLocalPathsWithoutTransferProgress(t *testing.T) {
	ctx := context.Background()
	backing, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", LegacyMediaLayout{}, map[string]Store{"local": backing})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backing.Put(ctx, mustParseKey(t, "file/video/out0.ts"), strings.NewReader("video"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	paths, cleanup, err := service.MaterializePrefixes(ctx, "local", []PrefixMaterialization{
		{Prefix: mustParseKey(t, "file/video"), Purpose: "download-quality"},
	}, func(float64) { t.Error("local paths must not report transfer progress") })
	if err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(paths[0], "out0.ts")); err != nil || string(data) != "video" {
		t.Fatalf("local source after cleanup = %q, %v", data, err)
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

func TestMaterializeRemovesScratchFileAfterSizeMismatch(t *testing.T) {
	ctx := context.Background()
	backing, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	workspace, err := NewLocalWorkspace(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithWorkspace("remote", LegacyMediaLayout{}, workspace, map[string]Store{
		"remote": wrongSizeStore{Store: backing},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := JoinKey("file", "source", "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backing.Put(ctx, key, strings.NewReader("source"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Materialize(ctx, "remote", key, "encoder-source", ".mp4"); err == nil {
		t.Fatal("Materialize() error = nil, want size mismatch")
	}
	entries, err := os.ReadDir(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("workspace retained %d entries after failure", len(entries))
	}
}

func TestPublishPrefixWritesManifestLastAndKeepsStaleObjectsOnFailure(t *testing.T) {
	ctx := context.Background()
	backing, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService("remote", LegacyMediaLayout{}, map[string]Store{
		"remote": failManifestStore{Store: backing},
	})
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := JoinKey("file", "720p")
	if err != nil {
		t.Fatal(err)
	}
	manifest := mustParseKey(t, "file/720p/stream.m3u8")
	segment := mustParseKey(t, "file/720p/out0.ts")
	stale := mustParseKey(t, "file/720p/stale.ts")
	for key, value := range map[Key]string{
		manifest: "old manifest",
		segment:  "old segment",
		stale:    "stale segment",
	} {
		if _, err := backing.Put(ctx, key, strings.NewReader(value), PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	sourceRoot := t.TempDir()
	mustWriteStorageTestFile(t, filepath.Join(sourceRoot, "out0.ts"), "new segment")
	mustWriteStorageTestFile(t, filepath.Join(sourceRoot, "stream.m3u8"), "new manifest")

	if _, err := service.PublishPrefix(ctx, "remote", prefix, sourceRoot, PutOptions{}); err == nil {
		t.Fatal("PublishPrefix() error = nil, want injected failure")
	}
	if got := readStorageTestObject(t, backing, manifest); got != "old manifest" {
		t.Fatalf("manifest = %q, want old manifest", got)
	}
	if got := readStorageTestObject(t, backing, segment); got != "new segment" {
		t.Fatalf("segment = %q, want new segment", got)
	}
	if _, err := backing.Stat(ctx, stale); err != nil {
		t.Fatalf("stale object was removed before successful publish: %v", err)
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

func readStorageTestObject(t *testing.T, store Store, key Key) string {
	t.Helper()
	object, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
