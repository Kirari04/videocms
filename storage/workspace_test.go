package storage

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestLocalWorkspaceCreatesAndCleansScratchResources(t *testing.T) {
	workspace, err := NewLocalWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalWorkspace() error = %v", err)
	}
	file, cleanupFile, err := workspace.TempFile(context.Background(), "thumbnail-input", ".png")
	if err != nil {
		t.Fatalf("TempFile() error = %v", err)
	}
	filePath := file.Name()
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := cleanupFile(); err != nil {
		t.Fatalf("cleanup file error = %v", err)
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file still exists or stat failed unexpectedly: %v", err)
	}

	directory, cleanupDir, err := workspace.TempDir(context.Background(), "generated-thumbnail")
	if err != nil {
		t.Fatalf("TempDir() error = %v", err)
	}
	if err := cleanupDir(); err != nil {
		t.Fatalf("cleanup directory error = %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary directory still exists or stat failed unexpectedly: %v", err)
	}
}

func TestLocalWorkspaceRejectsUnsafePurpose(t *testing.T) {
	workspace, err := NewLocalWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalWorkspace() error = %v", err)
	}
	if _, _, err := workspace.TempFile(context.Background(), "../escape", ""); err == nil {
		t.Fatal("TempFile() error = nil, want unsafe purpose rejection")
	}
}

func TestLocalWorkspaceCleansOnlyExpiredPurposeFiles(t *testing.T) {
	workspace, err := NewLocalWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expired, _, err := workspace.TempFile(context.Background(), "playback-cache", "")
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := workspace.TempFile(context.Background(), "playback-cache", "")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := workspace.TempFile(context.Background(), "thumbnail-input", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []*os.File{expired, current, other} {
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(expired.Name(), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other.Name(), old, old); err != nil {
		t.Fatal(err)
	}
	if err := workspace.CleanupTemporaryFiles(context.Background(), "playback-cache", time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expired.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired capture remains: %v", err)
	}
	for _, path := range []string{current.Name(), other.Name()} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated or current file was removed: %v", err)
		}
	}
}
