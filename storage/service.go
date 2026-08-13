package storage

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrStoreNotConfigured = errors.New("storage store not configured")

// Service is the runtime registry for named stores and the active media key
// layout. Named stores allow records to retain their original location during
// a gradual migration to another backend.
type Service struct {
	mu             sync.RWMutex
	defaultStoreID string
	stores         map[string]Store
	layout         MediaLayout
	workspace      Workspace
}

func NewService(defaultStoreID string, layout MediaLayout, stores map[string]Store) (*Service, error) {
	workspace, err := NewLocalWorkspace("")
	if err != nil {
		return nil, err
	}
	return NewServiceWithWorkspace(defaultStoreID, layout, workspace, stores)
}

func NewServiceWithWorkspace(defaultStoreID string, layout MediaLayout, workspace Workspace, stores map[string]Store) (*Service, error) {
	if defaultStoreID == "" || layout == nil || workspace == nil {
		return nil, ErrStoreNotConfigured
	}
	defaultStore, ok := stores[defaultStoreID]
	if !ok || defaultStore == nil {
		return nil, fmt.Errorf("%w: %s", ErrStoreNotConfigured, defaultStoreID)
	}
	owned := make(map[string]Store, len(stores))
	for id, store := range stores {
		if id == "" || store == nil {
			return nil, ErrStoreNotConfigured
		}
		owned[id] = store
	}
	return &Service{
		defaultStoreID: defaultStoreID,
		stores:         owned,
		layout:         layout,
		workspace:      workspace,
	}, nil
}

func (s *Service) DefaultStoreID() string {
	if s == nil {
		return ""
	}
	return s.defaultStoreID
}

func (s *Service) Default() (Store, error) {
	if s == nil {
		return nil, ErrStoreNotConfigured
	}
	return s.Store(s.defaultStoreID)
}

func (s *Service) Store(id string) (Store, error) {
	if s == nil {
		return nil, ErrStoreNotConfigured
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	store, ok := s.stores[id]
	if !ok || store == nil {
		return nil, fmt.Errorf("%w: %s", ErrStoreNotConfigured, id)
	}
	return store, nil
}

// RegisterStore atomically mounts or replaces a named store. The returned
// previous store is no longer discoverable but may still be in use by a
// request that resolved it before the replacement, so callers must not close
// it until those operations have drained.
func (s *Service) RegisterStore(id string, store Store) (Store, error) {
	if s == nil || id == "" || store == nil {
		return nil, ErrStoreNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.stores[id]
	s.stores[id] = store
	return previous, nil
}

// UnregisterStore removes an additional mount from future resolution. The
// built-in default store cannot be unregistered.
func (s *Service) UnregisterStore(id string) (Store, error) {
	if s == nil || id == "" {
		return nil, ErrStoreNotConfigured
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.defaultStoreID {
		return nil, fmt.Errorf("cannot unmount built-in store %q", id)
	}
	store, ok := s.stores[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrStoreNotConfigured, id)
	}
	delete(s.stores, id)
	return store, nil
}

func (s *Service) StoreIDs() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.stores))
	for id := range s.stores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// StoreOrDefault resolves a persisted store ID. Empty IDs are treated as the
// active default for compatibility with records created before store IDs were
// persisted.
func (s *Service) StoreOrDefault(id string) (Store, error) {
	if id == "" {
		return s.Default()
	}
	return s.Store(id)
}

func (s *Service) Layout() MediaLayout {
	if s == nil {
		return nil
	}
	return s.layout
}

func (s *Service) Workspace() Workspace {
	if s == nil {
		return nil
	}
	return s.workspace
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	stores := make(map[string]Store, len(s.stores))
	for id, store := range s.stores {
		stores[id] = store
	}
	s.mu.RUnlock()
	ids := make([]string, 0, len(stores))
	for id := range stores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var closeErr error
	for _, id := range ids {
		closeErr = errors.Join(closeErr, stores[id].Close())
	}
	return closeErr
}
