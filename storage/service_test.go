package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceMountRegistry(t *testing.T) {
	local, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService("local", LegacyMediaLayout{}, map[string]Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	remoteLocal, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := &closeTrackingStore{Store: remoteLocal}
	if err := service.RegisterStore("remote", remote); err != nil {
		t.Fatalf("RegisterStore() error = %v", err)
	}
	if got := service.StoreIDs(); len(got) != 2 || got[0] != "local" || got[1] != "remote" {
		t.Fatalf("StoreIDs() = %v", got)
	}
	if err := service.UnregisterStore("remote"); err != nil {
		t.Fatalf("UnregisterStore() error = %v", err)
	}
	if _, err := service.Store("remote"); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("removed store error = %v", err)
	}
	if got := remote.CloseCount(); got != 1 {
		t.Fatalf("removed store close count = %d, want 1", got)
	}
	if err := service.UnregisterStore("local"); err == nil {
		t.Fatal("UnregisterStore(local) error = nil")
	}
}

func TestServiceReplacementDrainsOpenObjectsBeforeClosingStore(t *testing.T) {
	local, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldLocal, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParseKey("video/segment.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oldLocal.Put(context.Background(), key, strings.NewReader("segment"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	oldStore := &closeTrackingStore{Store: oldLocal}
	service, err := NewService("local", LegacyMediaLayout{}, map[string]Store{
		"local":  local,
		"remote": oldStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	resolvedOld, err := service.Store("remote")
	if err != nil {
		t.Fatal(err)
	}
	object, err := resolvedOld.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}

	replacementLocal, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	replacement := &closeTrackingStore{Store: replacementLocal}
	if err := service.RegisterStore("remote", replacement); err != nil {
		t.Fatal(err)
	}
	if got := oldStore.CloseCount(); got != 0 {
		t.Fatalf("old store closed with an open object: close count = %d", got)
	}
	if _, err := resolvedOld.Stat(context.Background(), key); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("retired store accepted a new operation: %v", err)
	}
	if resolvedNew, err := service.Store("remote"); err != nil {
		t.Fatal(err)
	} else if resolvedNew == resolvedOld {
		t.Fatal("replacement resolved to the retired store")
	}

	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatalf("second object close returned an error: %v", err)
	}
	if got := oldStore.CloseCount(); got != 1 {
		t.Fatalf("drained store close count = %d, want 1", got)
	}

	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if got := oldStore.CloseCount(); got != 1 {
		t.Fatalf("old store close count after service close = %d, want 1", got)
	}
	if got := replacement.CloseCount(); got != 1 {
		t.Fatalf("replacement close count = %d, want 1", got)
	}
}

func TestServiceCloseWaitsForOpenObjects(t *testing.T) {
	local, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParseKey("video/source.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Put(context.Background(), key, strings.NewReader("source"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	tracked := &closeTrackingStore{Store: local}
	service, err := NewService("local", LegacyMediaLayout{}, map[string]Store{"local": tracked})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := service.Default()
	if err != nil {
		t.Fatal(err)
	}
	object, err := resolved.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan error, 1)
	go func() {
		closed <- service.Close()
	}()
	deadline := time.After(time.Second)
	for {
		_, storeErr := service.Default()
		if errors.Is(storeErr, ErrStorageServiceClosed) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("storage service did not begin closing")
		default:
		}
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before the open object drained: %v", err)
	default:
	}
	if got := tracked.CloseCount(); got != 0 {
		t.Fatalf("store close count with open object = %d, want 0", got)
	}

	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the object drained")
	}
	if got := tracked.CloseCount(); got != 1 {
		t.Fatalf("store close count = %d, want 1", got)
	}
}

func TestServiceCloseReportsErrorsFromPreviouslyRetiredStores(t *testing.T) {
	local, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteLocal, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("close remote connection")
	remote := &closeTrackingStore{Store: remoteLocal, closeErr: wantErr}
	service, err := NewService("local", LegacyMediaLayout{}, map[string]Store{
		"local":  local,
		"remote": remote,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UnregisterStore("remote"); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want %v", err, wantErr)
	}
	if got := remote.CloseCount(); got != 1 {
		t.Fatalf("retired store close count = %d, want 1", got)
	}
}

type closeTrackingStore struct {
	Store
	mu         sync.Mutex
	closeCount int
	closeErr   error
}

func (s *closeTrackingStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCount++
	return s.closeErr
}

func (s *closeTrackingStore) CloseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCount
}
