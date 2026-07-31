package storage

import (
	"context"
	"errors"
	"os"
	"testing"
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
