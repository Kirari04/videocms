package storage

import (
	"errors"
	"fmt"
	"sort"
)

var ErrStoreNotConfigured = errors.New("storage store not configured")

// Service is the runtime registry for named stores and the active media key
// layout. Named stores allow records to retain their original location during
// a gradual migration to another backend.
type Service struct {
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
	store, ok := s.stores[id]
	if !ok || store == nil {
		return nil, fmt.Errorf("%w: %s", ErrStoreNotConfigured, id)
	}
	return store, nil
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
	ids := make([]string, 0, len(s.stores))
	for id := range s.stores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var closeErr error
	for _, id := range ids {
		closeErr = errors.Join(closeErr, s.stores[id].Close())
	}
	return closeErr
}
