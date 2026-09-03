package storage

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrStoreNotConfigured = errors.New("storage store not configured")
var ErrStorageServiceClosed = errors.New("storage service is closed")

// Service is the runtime registry for named stores and the active media key
// layout. Named stores allow records to retain their original location during
// a gradual migration to another backend.
type Service struct {
	mu             sync.RWMutex
	defaultStoreID string
	stores         map[string]*managedStore
	retired        map[*managedStore]struct{}
	closeErr       error
	closed         bool
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
	service := &Service{
		defaultStoreID: defaultStoreID,
		stores:         make(map[string]*managedStore, len(stores)),
		retired:        make(map[*managedStore]struct{}),
		layout:         layout,
		workspace:      workspace,
	}
	for id, store := range stores {
		if id == "" || store == nil {
			return nil, ErrStoreNotConfigured
		}
		service.stores[id] = newManagedStore(id, store, service.storeClosed)
	}
	return service, nil
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
	if s.closed {
		return nil, ErrStorageServiceClosed
	}
	store, ok := s.stores[id]
	if !ok || store == nil {
		return nil, fmt.Errorf("%w: %s", ErrStoreNotConfigured, id)
	}
	return store.resolved(), nil
}

// RegisterStore atomically mounts or replaces a named store. A replaced store
// is retired automatically: new operations resolve the replacement while
// active operations drain before the old provider is closed.
func (s *Service) RegisterStore(id string, store Store) error {
	if s == nil || id == "" || store == nil {
		return ErrStoreNotConfigured
	}
	managed := newManagedStore(id, store, s.storeClosed)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStorageServiceClosed
	}
	previous := s.stores[id]
	s.stores[id] = managed
	if previous != nil {
		s.retired[previous] = struct{}{}
	}
	s.mu.Unlock()
	if previous != nil {
		previous.retire()
	}
	return nil
}

// UnregisterStore removes an additional mount from future resolution. The
// built-in default store cannot be unregistered. The removed store is closed
// automatically after its active operations finish.
func (s *Service) UnregisterStore(id string) error {
	if s == nil || id == "" {
		return ErrStoreNotConfigured
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStorageServiceClosed
	}
	if id == s.defaultStoreID {
		s.mu.Unlock()
		return fmt.Errorf("cannot unmount built-in store %q", id)
	}
	store, ok := s.stores[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrStoreNotConfigured, id)
	}
	delete(s.stores, id)
	s.retired[store] = struct{}{}
	s.mu.Unlock()
	store.retire()
	return nil
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
	s.mu.Lock()
	s.closed = true
	for id, store := range s.stores {
		s.retired[store] = struct{}{}
		delete(s.stores, id)
	}
	stores := make([]*managedStore, 0, len(s.retired))
	for store := range s.retired {
		stores = append(stores, store)
	}
	s.mu.Unlock()

	for _, store := range stores {
		store.retire()
	}
	for _, store := range stores {
		_ = store.waitClosed()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

func (s *Service) storeClosed(store *managedStore, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.retired, store)
	s.closeErr = errors.Join(s.closeErr, err)
}
