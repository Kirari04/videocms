package storage

import (
	"context"
	"errors"
)

// DeletePrefix removes every object below prefix. It is intentionally built
// from Walk and Delete so adapters do not have to emulate filesystem
// RemoveAll semantics.
func DeletePrefix(ctx context.Context, store Store, prefix Key) error {
	if store == nil {
		return ErrStoreNotConfigured
	}
	keys := make([]Key, 0)
	if err := store.Walk(ctx, prefix, func(info ObjectInfo) error {
		keys = append(keys, info.Key)
		return nil
	}); err != nil {
		return err
	}
	var deleteErr error
	for _, key := range keys {
		if err := store.Delete(ctx, key); err != nil {
			deleteErr = errors.Join(deleteErr, err)
		}
	}
	return deleteErr
}
