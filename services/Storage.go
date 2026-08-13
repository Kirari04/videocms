package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
)

func (w *WorkerGroup) materializeEncodingSource(ctx context.Context, file models.File) (string, func() error, error) {
	if file.SourceKey != "" {
		if w.deps == nil || w.deps.Storage == nil {
			return "", nil, storage.ErrStoreNotConfigured
		}
		key, err := storage.ParseKey(file.SourceKey)
		if err != nil {
			return "", nil, err
		}
		return w.deps.Storage.Materialize(ctx, file.StorageID, key, "encoder-source", filepath.Ext(key.String()))
	}
	if file.Path == "" {
		return "", nil, fmt.Errorf("%w: source for file %s", storage.ErrNotFound, file.UUID)
	}
	path, err := filepath.Abs(file.Path)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("source is not a regular file: %s", path)
	}
	return path, func() error { return nil }, nil
}

func (w *WorkerGroup) encodingOutputDirectory(ctx context.Context, purpose, legacyPath string) (string, func() error, error) {
	if w.deps != nil && w.deps.Storage != nil && w.deps.Storage.Workspace() != nil {
		return w.deps.Storage.Workspace().TempDir(ctx, purpose)
	}
	path, err := filepath.Abs(legacyPath)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(path, 0o777); err != nil {
		return "", nil, err
	}
	return path, func() error { return nil }, nil
}

func (w *WorkerGroup) publishEncodingOutput(ctx context.Context, file models.File, prefix storage.Key, directory string) error {
	if w.deps == nil || w.deps.Storage == nil {
		return nil
	}
	_, err := w.deps.Storage.PublishPrefix(ctx, file.StorageID, prefix, directory, storage.PutOptions{
		CacheControl: "public, max-age=3600",
	})
	return err
}
