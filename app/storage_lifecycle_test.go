package app

import "testing"

func TestStorageLifecycleReleasesUnusedKeyedLocks(t *testing.T) {
	var lifecycle StorageLifecycle
	first := lifecycle.ReadLock("mount-a")
	second := lifecycle.ReadLock("mount-a")
	if len(lifecycle.locks) != 1 || lifecycle.locks["mount-a"].refs != 2 {
		t.Fatalf("unexpected retained lock state: %#v", lifecycle.locks)
	}
	first()
	if len(lifecycle.locks) != 1 || lifecycle.locks["mount-a"].refs != 1 {
		t.Fatalf("lock was released while still referenced: %#v", lifecycle.locks)
	}
	second()
	if len(lifecycle.locks) != 0 {
		t.Fatalf("unused lifecycle lock was retained: %#v", lifecycle.locks)
	}
}
