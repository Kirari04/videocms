package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var safePurpose = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var safeSuffix = regexp.MustCompile(`^(\.[a-z0-9]{1,16})?$`)

// Workspace represents explicitly local, non-authoritative scratch space for
// path-oriented tools such as FFmpeg and ffprobe.
type Workspace interface {
	TempFile(ctx context.Context, purpose string, suffix string) (*os.File, func() error, error)
	TempDir(ctx context.Context, purpose string) (string, func() error, error)
}

type LocalWorkspace struct {
	root string
}

func NewLocalWorkspace(root string) (*LocalWorkspace, error) {
	if root == "" {
		root = os.TempDir()
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	return &LocalWorkspace{root: absRoot}, nil
}

func (w *LocalWorkspace) TempFile(ctx context.Context, purpose string, suffix string) (*os.File, func() error, error) {
	if err := validateWorkspaceRequest(ctx, w, purpose); err != nil {
		return nil, nil, err
	}
	if !safeSuffix.MatchString(suffix) {
		return nil, nil, fmt.Errorf("invalid workspace suffix %q", suffix)
	}
	file, err := os.CreateTemp(w.root, "videocms-"+purpose+"-*"+suffix)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() error {
		err := os.Remove(file.Name())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return file, cleanup, nil
}

func (w *LocalWorkspace) TempDir(ctx context.Context, purpose string) (string, func() error, error) {
	if err := validateWorkspaceRequest(ctx, w, purpose); err != nil {
		return "", nil, err
	}
	directory, err := os.MkdirTemp(w.root, "videocms-"+purpose+"-*")
	if err != nil {
		return "", nil, err
	}
	return directory, func() error { return os.RemoveAll(directory) }, nil
}

func validateWorkspaceRequest(ctx context.Context, workspace *LocalWorkspace, purpose string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if workspace == nil || workspace.root == "" {
		return ErrStoreNotConfigured
	}
	if !safePurpose.MatchString(purpose) {
		return fmt.Errorf("invalid workspace purpose %q", purpose)
	}
	return nil
}
