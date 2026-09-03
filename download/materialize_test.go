package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
)

type remoteLikeStore struct{ storage.Store }

func TestMaterializeSelectionPreservesRenditionTrees(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := storage.NewLocalWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := storage.NewServiceWithWorkspace(
		"remote",
		storage.LegacyMediaLayout{},
		workspace,
		map[string]storage.Store{"remote": remoteLikeStore{Store: store}},
	)
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string]string{
		"file/720p/out.m3u8":    "video manifest",
		"file/720p/out0.ts":     "video segment",
		"file/audio/audio.m3u8": "audio manifest",
		"file/audio/audio0.ts":  "audio segment",
		"file/subtitle/out.vtt": "subtitle",
	}
	for rawKey, value := range objects {
		key, err := storage.ParseKey(rawKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Put(ctx, key, strings.NewReader(value), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	file := models.File{UUID: "file", StorageID: "remote"}
	selection := &Selection{
		Quality:   models.Quality{Name: "720p", OutputFile: "out.m3u8"},
		Audios:    []models.Audio{{UUID: "audio", OutputFile: "audio.m3u8"}},
		Subtitles: []models.Subtitle{{UUID: "subtitle", OutputFile: "out.vtt"}},
	}
	cleanup, err := MaterializeSelection(ctx, service, &file, selection)
	if err != nil {
		t.Fatal(err)
	}
	qualityPath := selection.Quality.Path
	if data, err := os.ReadFile(filepath.Join(qualityPath, "out0.ts")); err != nil || string(data) != "video segment" {
		t.Fatalf("materialized quality segment = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(selection.Audios[0].Path, "audio0.ts")); err != nil || string(data) != "audio segment" {
		t.Fatalf("materialized audio segment = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(selection.Subtitles[0].Path, "out.vtt")); err != nil || string(data) != "subtitle" {
		t.Fatalf("materialized subtitle = %q, %v", data, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(qualityPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("materialized quality was not cleaned up: %v", err)
	}
}
