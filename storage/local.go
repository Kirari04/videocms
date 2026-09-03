package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
)

type LocalStore struct {
	root string
}

// LocalPath exposes a read-only path optimization to the storage service.
// It is intentionally not part of Store because remote adapters cannot
// provide this capability.
func (s *LocalStore) LocalPath(ctx context.Context, key Key) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	objectPath, err := s.pathFor(key)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(objectPath); err != nil {
		return "", normalizeLocalError(key, err)
	}
	return objectPath, nil
}

func NewLocalStore(root string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("local storage root is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o766); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect local storage root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local storage root is not a directory: %s", absRoot)
	}
	return &LocalStore{root: absRoot}, nil
}

func (s *LocalStore) Open(ctx context.Context, key Key) (*Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	objectPath, err := s.pathFor(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(objectPath)
	if err != nil {
		return nil, normalizeLocalError(key, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, normalizeLocalError(key, err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key.String())
	}
	return &Object{
		Body: file,
		Info: localObjectInfo(key, info),
	}, nil
}

func (s *LocalStore) Capacity(ctx context.Context) (CapacityInfo, error) {
	if err := ctx.Err(); err != nil {
		return CapacityInfo{}, err
	}
	if s == nil || s.root == "" {
		return CapacityInfo{}, ErrStoreNotConfigured
	}
	usage, err := disk.Usage(s.root)
	if err != nil {
		return CapacityInfo{}, fmt.Errorf("inspect local storage capacity: %w", err)
	}
	return CapacityInfo{Total: usage.Total, Free: usage.Free}, nil
}

func (s *LocalStore) Put(ctx context.Context, key Key, src io.Reader, opts PutOptions) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	objectPath, err := s.pathFor(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o766); err != nil {
		return ObjectInfo{}, fmt.Errorf("create object directory: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(objectPath), ".videocms-put-*")
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("create temporary object: %w", err)
	}
	tempPath := tempFile.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	written, copyErr := copyWithContext(ctx, tempFile, src)
	closeErr := tempFile.Close()
	if copyErr != nil {
		return ObjectInfo{}, fmt.Errorf("write object %s: %w", key.String(), copyErr)
	}
	if closeErr != nil {
		return ObjectInfo{}, fmt.Errorf("close object %s: %w", key.String(), closeErr)
	}
	if opts.ExpectedSize != nil && written != *opts.ExpectedSize {
		return ObjectInfo{}, fmt.Errorf("object %s size mismatch: wrote %d, expected %d", key.String(), written, *opts.ExpectedSize)
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return ObjectInfo{}, fmt.Errorf("set object permissions: %w", err)
	}
	if err := os.Rename(tempPath, objectPath); err != nil {
		return ObjectInfo{}, fmt.Errorf("publish object %s: %w", key.String(), err)
	}
	keepTemp = true
	return s.Stat(ctx, key)
}

func (s *LocalStore) Stat(ctx context.Context, key Key) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	objectPath, err := s.pathFor(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := os.Stat(objectPath)
	if err != nil {
		return ObjectInfo{}, normalizeLocalError(key, err)
	}
	if !info.Mode().IsRegular() {
		return ObjectInfo{}, fmt.Errorf("%w: %s", ErrNotFound, key.String())
	}
	return localObjectInfo(key, info), nil
}

func (s *LocalStore) Delete(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	objectPath, err := s.pathFor(key)
	if err != nil {
		return err
	}
	if err := os.Remove(objectPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object %s: %w", key.String(), err)
	}
	s.pruneEmptyParents(filepath.Dir(objectPath))
	return nil
}

func (s *LocalStore) Walk(ctx context.Context, prefix Key, fn func(ObjectInfo) error) error {
	if fn == nil {
		return errors.New("storage walk callback is nil")
	}
	prefixPath, err := s.pathFor(prefix)
	if err != nil {
		return err
	}
	err = filepath.WalkDir(prefixPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && currentPath == prefixPath {
				return fs.SkipAll
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(s.root, currentPath)
		if err != nil {
			return err
		}
		key, err := ParseKey(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		return fn(localObjectInfo(key, info))
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *LocalStore) Close() error {
	return nil
}

func (s *LocalStore) pruneEmptyParents(directory string) {
	for directory != s.root {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}

func (s *LocalStore) pathFor(key Key) (string, error) {
	if s == nil || s.root == "" {
		return "", ErrStoreNotConfigured
	}
	validated, err := ParseKey(key.String())
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(s.root, filepath.FromSlash(validated.String()))
	relative, err := filepath.Rel(s.root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s", ErrInvalidKey, key.String())
	}
	return candidate, nil
}

func localObjectInfo(key Key, info fs.FileInfo) ObjectInfo {
	return ObjectInfo{
		Key:         key,
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		ContentType: mime.TypeByExtension(filepath.Ext(key.String())),
	}
}

func normalizeLocalError(key Key, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, key.String())
	}
	return err
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		read, readErr := src.Read(buffer)
		if read > 0 {
			count, writeErr := dst.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != read {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}
