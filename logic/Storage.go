package logic

import (
	"ch/kirari04/videocms/storage"
)

func (s *Service) mediaStorage(storeID string) (storage.Store, storage.MediaLayout, error) {
	if s == nil || s.Deps == nil || s.Deps.Storage == nil || s.Deps.Storage.Layout() == nil {
		return nil, nil, storage.ErrStoreNotConfigured
	}
	store, err := s.Deps.Storage.StoreOrDefault(storeID)
	if err != nil {
		return nil, nil, err
	}
	return store, s.Deps.Storage.Layout(), nil
}
