package storage

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type localPathStore interface {
	LocalPath(context.Context, Key) (string, error)
}

// Materialize copies an object into local scratch storage when the selected
// adapter cannot expose a local path. The returned cleanup must be called.
func (s *Service) Materialize(ctx context.Context, storeID string, key Key, purpose, suffix string) (string, func() error, error) {
	store, err := s.StoreOrDefault(storeID)
	if err != nil {
		return "", nil, err
	}
	if local, ok := store.(localPathStore); ok {
		path, err := local.LocalPath(ctx, key)
		if err != nil {
			return "", nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, err
		}
		if !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("%w: %s", ErrNotFound, key.String())
		}
		return path, noCleanup, nil
	}

	object, err := store.Open(ctx, key)
	if err != nil {
		return "", nil, err
	}
	defer object.Body.Close()

	temporary, cleanup, err := s.workspace.TempFile(ctx, purpose, suffix)
	if err != nil {
		return "", nil, err
	}
	path := temporary.Name()
	written, copyErr := copyWithContext(ctx, temporary, object.Body)
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil || written != object.Info.Size {
		_ = cleanup()
		if copyErr != nil {
			return "", nil, copyErr
		}
		if closeErr != nil {
			return "", nil, closeErr
		}
		return "", nil, fmt.Errorf("materialized object %s size mismatch: wrote %d, expected %d", key.String(), written, object.Info.Size)
	}
	return path, cleanup, nil
}

// MaterializePrefix makes a complete object prefix available as a local
// directory. Relative object names are preserved for manifests such as HLS.
func (s *Service) MaterializePrefix(ctx context.Context, storeID string, prefix Key, purpose string) (string, func() error, error) {
	store, err := s.StoreOrDefault(storeID)
	if err != nil {
		return "", nil, err
	}
	if local, ok := store.(localPathStore); ok {
		path, err := local.LocalPath(ctx, prefix)
		if err != nil {
			return "", nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, err
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("%w: %s", ErrNotFound, prefix.String())
		}
		return path, noCleanup, nil
	}

	directory, cleanup, err := s.workspace.TempDir(ctx, purpose)
	if err != nil {
		return "", nil, err
	}
	found := false
	err = store.Walk(ctx, prefix, func(info ObjectInfo) error {
		relative, err := relativeObjectKey(prefix, info.Key)
		if err != nil {
			return err
		}
		destination := filepath.Join(directory, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		object, err := store.Open(ctx, info.Key)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			object.Body.Close()
			return err
		}
		written, copyErr := copyWithContext(ctx, file, object.Body)
		closeErr := errors.Join(file.Close(), object.Body.Close())
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != object.Info.Size {
			return fmt.Errorf("materialized object %s size mismatch: wrote %d, expected %d", info.Key.String(), written, object.Info.Size)
		}
		found = true
		return nil
	})
	if err != nil || !found {
		_ = cleanup()
		if err != nil {
			return "", nil, err
		}
		return "", nil, fmt.Errorf("%w: %s", ErrNotFound, prefix.String())
	}
	return directory, cleanup, nil
}

func (s *Service) PublishFile(ctx context.Context, storeID string, key Key, localPath string, opts PutOptions) (ObjectInfo, error) {
	store, err := s.StoreOrDefault(storeID)
	if err != nil {
		return ObjectInfo{}, err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ObjectInfo{}, err
	}
	if !info.Mode().IsRegular() {
		return ObjectInfo{}, fmt.Errorf("publish source is not a regular file: %s", localPath)
	}
	expectedSize := info.Size()
	opts.ExpectedSize = &expectedSize
	if opts.ContentType == "" {
		opts.ContentType = mime.TypeByExtension(filepath.Ext(localPath))
	}
	return store.Put(ctx, key, file, opts)
}

// PublishPrefix publishes regular files under localRoot while preserving their
// relative names. Manifest files are written last, then stale remote objects
// are removed only after the replacement tree is complete.
func (s *Service) PublishPrefix(ctx context.Context, storeID string, prefix Key, localRoot string, opts PutOptions) ([]ObjectInfo, error) {
	store, err := s.StoreOrDefault(storeID)
	if err != nil {
		return nil, err
	}
	entries, err := publishEntries(localRoot)
	if err != nil {
		return nil, err
	}
	published := make([]ObjectInfo, 0, len(entries))
	publishedKeys := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key, err := ParseKey(prefix.String() + "/" + entry.relative)
		if err != nil {
			return nil, err
		}
		info, err := s.PublishFile(ctx, storeID, key, entry.path, opts)
		if err != nil {
			return nil, err
		}
		published = append(published, info)
		publishedKeys[key.String()] = true
	}
	var stale []Key
	if err := store.Walk(ctx, prefix, func(info ObjectInfo) error {
		if publishedKeys[info.Key.String()] {
			return nil
		}
		stale = append(stale, info.Key)
		return nil
	}); err != nil {
		return nil, err
	}
	for _, key := range stale {
		if err := store.Delete(ctx, key); err != nil {
			return nil, err
		}
	}
	return published, nil
}

type publishEntry struct {
	path     string
	relative string
	manifest bool
}

func publishEntries(root string) ([]publishEntry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var entries []publishEntry
	err = filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("publish tree contains symlink: %s", current)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("publish tree contains non-regular file: %s", current)
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, err := ParseKey(relative); err != nil {
			return err
		}
		entries = append(entries, publishEntry{
			path:     current,
			relative: relative,
			manifest: strings.EqualFold(filepath.Ext(relative), ".m3u8"),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("publish tree is empty")
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].manifest != entries[j].manifest {
			return !entries[i].manifest
		}
		return entries[i].relative < entries[j].relative
	})
	return entries, nil
}

func relativeObjectKey(prefix, key Key) (string, error) {
	base := prefix.String() + "/"
	if !strings.HasPrefix(key.String(), base) {
		return "", fmt.Errorf("object %s is outside prefix %s", key.String(), prefix.String())
	}
	relative := strings.TrimPrefix(key.String(), base)
	if _, err := ParseKey(relative); err != nil {
		return "", err
	}
	return relative, nil
}

func noCleanup() error { return nil }
