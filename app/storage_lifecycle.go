package app

import (
	"fmt"
	"sort"
	"sync"
)

// StorageLifecycle serializes storage, pool, and per-file transitions that
// must not overlap. The zero value is ready to use. Independent resources use
// separate keyed locks, and unused entries are released from the lock map.
type StorageLifecycle struct {
	mu    sync.Mutex
	locks map[string]*storageLifecycleLock
}

type storageLifecycleLock struct {
	lock sync.RWMutex
	refs int
}

func (l *StorageLifecycle) ReadLock(mountID string) func() {
	entry := l.retainLock(mountID)
	entry.lock.RLock()
	return func() {
		entry.lock.RUnlock()
		l.releaseLock(mountID, entry)
	}
}

func (l *StorageLifecycle) WriteLock(mountID string) func() {
	entry := l.retainLock(mountID)
	entry.lock.Lock()
	return func() {
		entry.lock.Unlock()
		l.releaseLock(mountID, entry)
	}
}

func (l *StorageLifecycle) ReadLocks(mountIDs ...string) func() {
	ids := uniqueLifecycleIDs(mountIDs)
	releases := make([]func(), 0, len(ids))
	for _, id := range ids {
		releases = append(releases, l.ReadLock(id))
	}
	return func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}
}

func (l *StorageLifecycle) FileReadLock(fileID uint) func() {
	return l.ReadLock(fmt.Sprintf("file:%d", fileID))
}

func (l *StorageLifecycle) FileReadLocks(fileIDs ...uint) func() {
	keys := make([]string, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		if fileID > 0 {
			keys = append(keys, fmt.Sprintf("file:%d", fileID))
		}
	}
	return l.ReadLocks(keys...)
}

func (l *StorageLifecycle) FileWriteLock(fileID uint) func() {
	return l.WriteLock(fmt.Sprintf("file:%d", fileID))
}

func (l *StorageLifecycle) PoolReadLock(poolID uint) func() {
	return l.ReadLock(fmt.Sprintf("pool:%d", poolID))
}

func (l *StorageLifecycle) PoolWriteLock(poolID uint) func() {
	return l.WriteLock(fmt.Sprintf("pool:%d", poolID))
}

func (l *StorageLifecycle) PoolReadLocks(poolIDs ...uint) func() {
	keys := make([]string, 0, len(poolIDs))
	for _, poolID := range poolIDs {
		if poolID > 0 {
			keys = append(keys, fmt.Sprintf("pool:%d", poolID))
		}
	}
	return l.ReadLocks(keys...)
}

func uniqueLifecycleIDs(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (l *StorageLifecycle) retainLock(mountID string) *storageLifecycleLock {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[string]*storageLifecycleLock)
	}
	entry := l.locks[mountID]
	if entry == nil {
		entry = &storageLifecycleLock{}
		l.locks[mountID] = entry
	}
	entry.refs++
	return entry
}

func (l *StorageLifecycle) releaseLock(mountID string, entry *storageLifecycleLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && l.locks[mountID] == entry {
		delete(l.locks, mountID)
	}
}
