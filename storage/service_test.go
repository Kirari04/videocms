package storage

import (
	"errors"
	"testing"
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
	remote, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if previous, err := service.RegisterStore("remote", remote); err != nil || previous != nil {
		t.Fatalf("RegisterStore() = %T, %v", previous, err)
	}
	if got := service.StoreIDs(); len(got) != 2 || got[0] != "local" || got[1] != "remote" {
		t.Fatalf("StoreIDs() = %v", got)
	}
	removed, err := service.UnregisterStore("remote")
	if err != nil || removed != remote {
		t.Fatalf("UnregisterStore() = %T, %v", removed, err)
	}
	if _, err := service.Store("remote"); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("removed store error = %v", err)
	}
	if _, err := service.UnregisterStore("local"); err == nil {
		t.Fatal("UnregisterStore(local) error = nil")
	}
}
