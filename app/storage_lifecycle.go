package app

import "sync"

// StorageLifecycle serializes operations that must not overlap a mount
// transition. The zero value is ready to use. Locks are scoped by durable
// mount ID so detaching one backend does not stall uploads to another.
type StorageLifecycle struct {
	mu    sync.Mutex
	locks map[string]*sync.RWMutex
}

func (l *StorageLifecycle) ReadLock(mountID string) func() {
	lock := l.mountLock(mountID)
	lock.RLock()
	return lock.RUnlock
}

func (l *StorageLifecycle) WriteLock(mountID string) func() {
	lock := l.mountLock(mountID)
	lock.Lock()
	return lock.Unlock
}

func (l *StorageLifecycle) mountLock(mountID string) *sync.RWMutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[string]*sync.RWMutex)
	}
	lock := l.locks[mountID]
	if lock == nil {
		lock = &sync.RWMutex{}
		l.locks[mountID] = lock
	}
	return lock
}
