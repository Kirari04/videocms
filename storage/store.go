package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrNotFound = errors.New("storage object not found")

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type Object struct {
	Body ReadSeekCloser
	Info ObjectInfo
}

type ObjectInfo struct {
	Key          Key
	Size         int64
	ModTime      time.Time
	ContentType  string
	CacheControl string
	ETag         string
}

type PutOptions struct {
	ContentType  string
	CacheControl string
	ExpectedSize *int64
}

// Store exposes only operations that have equivalent semantics on local disk
// and object storage. Directory creation and renames intentionally do not form
// part of this contract. Open must support repeated seeks efficiently; remote
// adapters should translate seeks into provider range requests rather than
// downloading the entire object. Delete is idempotent. Walk visits only the
// object at prefix or descendants below the prefix segment boundary.
type Store interface {
	Open(ctx context.Context, key Key) (*Object, error)
	Put(ctx context.Context, key Key, src io.Reader, opts PutOptions) (ObjectInfo, error)
	Stat(ctx context.Context, key Key) (ObjectInfo, error)
	Delete(ctx context.Context, key Key) error
	Walk(ctx context.Context, prefix Key, fn func(ObjectInfo) error) error
	Close() error
}

type HealthChecker interface {
	Check(context.Context) error
}
