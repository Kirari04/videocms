package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// managedStore keeps the underlying provider alive while an operation is in
// flight. Registry replacement and unmount retire the handle immediately, so
// new operations fail while existing operations are allowed to drain before
// the provider is closed.
type managedStore struct {
	id      string
	store   Store
	exposed Store

	mu       sync.Mutex
	active   int
	retired  bool
	closing  bool
	closeErr error
	done     chan struct{}
	onClosed func(*managedStore, error)
}

func newManagedStore(id string, store Store, onClosed func(*managedStore, error)) *managedStore {
	managed := &managedStore{
		id:       id,
		store:    store,
		done:     make(chan struct{}),
		onClosed: onClosed,
	}
	managed.exposed = managed
	if local, ok := store.(localPathStore); ok {
		managed.exposed = &managedLocalPathStore{managedStore: managed, local: local}
	}
	return managed
}

func (s *managedStore) resolved() Store {
	return s.exposed
}

func (s *managedStore) acquire() (Store, func(), error) {
	s.mu.Lock()
	if s.retired {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: %s is retired", ErrStoreNotConfigured, s.id)
	}
	s.active++
	store := s.store
	s.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(s.release)
	}
	return store, release, nil
}

func (s *managedStore) release() {
	s.mu.Lock()
	s.active--
	shouldClose := s.retired && s.active == 0 && !s.closing
	if shouldClose {
		s.closing = true
	}
	s.mu.Unlock()
	if shouldClose {
		s.finishClose()
	}
}

func (s *managedStore) retire() {
	s.mu.Lock()
	if s.retired {
		s.mu.Unlock()
		return
	}
	s.retired = true
	shouldClose := s.active == 0 && !s.closing
	if shouldClose {
		s.closing = true
	}
	s.mu.Unlock()
	if shouldClose {
		s.finishClose()
	}
}

func (s *managedStore) finishClose() {
	err := s.store.Close()
	s.mu.Lock()
	s.closeErr = err
	onClosed := s.onClosed
	s.mu.Unlock()
	if onClosed != nil {
		onClosed(s, err)
	}
	close(s.done)
}

func (s *managedStore) waitClosed() error {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

func (s *managedStore) Open(ctx context.Context, key Key) (*Object, error) {
	store, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	object, err := store.Open(ctx, key)
	if err != nil {
		release()
		return nil, err
	}
	if object == nil || object.Body == nil {
		release()
		return nil, errors.New("storage store returned an object without a body")
	}
	object.Body = &managedReadSeekCloser{ReadSeekCloser: object.Body, release: release}
	return object, nil
}

func (s *managedStore) Put(ctx context.Context, key Key, src io.Reader, opts PutOptions) (ObjectInfo, error) {
	store, release, err := s.acquire()
	if err != nil {
		return ObjectInfo{}, err
	}
	defer release()
	return store.Put(ctx, key, src, opts)
}

func (s *managedStore) Stat(ctx context.Context, key Key) (ObjectInfo, error) {
	store, release, err := s.acquire()
	if err != nil {
		return ObjectInfo{}, err
	}
	defer release()
	return store.Stat(ctx, key)
}

func (s *managedStore) Delete(ctx context.Context, key Key) error {
	store, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	return store.Delete(ctx, key)
}

func (s *managedStore) Walk(ctx context.Context, prefix Key, fn func(ObjectInfo) error) error {
	store, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	return store.Walk(ctx, prefix, fn)
}

func (s *managedStore) Check(ctx context.Context) error {
	store, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	checker, ok := store.(HealthChecker)
	if !ok {
		return nil
	}
	return checker.Check(ctx)
}

func (s *managedStore) Capacity(ctx context.Context) (CapacityInfo, error) {
	store, release, err := s.acquire()
	if err != nil {
		return CapacityInfo{}, err
	}
	defer release()
	reporter, ok := store.(CapacityReporter)
	if !ok {
		return CapacityInfo{}, ErrCapacityUnavailable
	}
	return reporter.Capacity(ctx)
}

func (s *managedStore) Close() error {
	s.retire()
	return s.waitClosed()
}

type managedLocalPathStore struct {
	*managedStore
	local localPathStore
}

func (s *managedLocalPathStore) LocalPath(ctx context.Context, key Key) (string, error) {
	_, release, err := s.acquire()
	if err != nil {
		return "", err
	}
	defer release()
	return s.local.LocalPath(ctx, key)
}

type managedReadSeekCloser struct {
	ReadSeekCloser
	release  func()
	once     sync.Once
	closeErr error
}

func (r *managedReadSeekCloser) Close() error {
	r.once.Do(func() {
		r.closeErr = r.ReadSeekCloser.Close()
		r.release()
	})
	return r.closeErr
}

var _ Store = (*managedStore)(nil)
var _ Store = (*managedLocalPathStore)(nil)
var _ HealthChecker = (*managedStore)(nil)
var _ CapacityReporter = (*managedStore)(nil)
