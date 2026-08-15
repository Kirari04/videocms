package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSFTPStoreIntegration(t *testing.T) {
	host := os.Getenv("VIDEOCMS_SFTP_INTEGRATION_HOST")
	if host == "" {
		t.Skip("VIDEOCMS_SFTP_INTEGRATION_HOST is not configured")
	}
	portValue := os.Getenv("VIDEOCMS_SFTP_INTEGRATION_PORT")
	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("VIDEOCMS_SFTP_INTEGRATION_PORT %q is invalid", portValue)
	}
	fingerprints := strings.FieldsFunc(
		os.Getenv("VIDEOCMS_SFTP_INTEGRATION_HOST_KEY_FINGERPRINTS"),
		func(value rune) bool { return value == ',' || value == '\n' || value == '\r' },
	)
	if len(fingerprints) == 0 {
		t.Fatal("VIDEOCMS_SFTP_INTEGRATION_HOST_KEY_FINGERPRINTS is required")
	}
	root := os.Getenv("VIDEOCMS_SFTP_INTEGRATION_ROOT")
	if root == "" {
		root = "/upload"
	}
	options := SFTPOptions{
		Host:                host,
		Port:                port,
		Username:            os.Getenv("VIDEOCMS_SFTP_INTEGRATION_USERNAME"),
		Root:                root,
		Authentication:      SFTPAuthenticationPassword,
		HostKeyFingerprints: fingerprints,
		Password:            os.Getenv("VIDEOCMS_SFTP_INTEGRATION_PASSWORD"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := NewSFTPStore(ctx, options)
	if err != nil {
		t.Fatalf("NewSFTPStore() error = %v", err)
	}
	cleanupStore := store
	prefix := "videocms-integration/" + uuid.NewString()
	key := mustParseKey(t, prefix+"/video/720p/out0.ts")
	siblingKey := mustParseKey(t, prefix+"/audio/audio0.ts")
	outsideKey := mustParseKey(t, "videocms-integration-other/"+uuid.NewString()+"/out0.ts")
	cleanupKeys := []Key{key, siblingKey, outsideKey}
	t.Cleanup(func() {
		for _, cleanupKey := range cleanupKeys {
			_ = cleanupStore.Delete(context.Background(), cleanupKey)
		}
		_ = cleanupStore.Close()
		if cleanupStore != store {
			_ = store.Close()
		}
	})

	if err := store.Check(ctx); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	initial := []byte("0123456789abcdef")
	initialSize := int64(len(initial))
	if _, err := store.Put(ctx, key, bytes.NewReader(initial), PutOptions{ExpectedSize: &initialSize}); err != nil {
		t.Fatalf("initial Put() error = %v", err)
	}
	info, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size != initialSize {
		t.Fatalf("Stat() size = %d, want %d", info.Size, initialSize)
	}
	object, err := store.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := object.Body.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	segment := make([]byte, 6)
	if _, err := io.ReadFull(object.Body, segment); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if closeErr := object.Body.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if string(segment) != "456789" {
		t.Fatalf("range read = %q, want %q", segment, "456789")
	}

	replacement := []byte("replacement through OpenSSH")
	replacementSize := int64(len(replacement))
	if _, err := store.Put(ctx, key, bytes.NewReader(replacement), PutOptions{ExpectedSize: &replacementSize}); err != nil {
		t.Fatalf("replacement Put() error = %v", err)
	}
	if _, err := store.Put(ctx, siblingKey, strings.NewReader("audio"), PutOptions{}); err != nil {
		t.Fatalf("sibling Put() error = %v", err)
	}
	if _, err := store.Put(ctx, outsideKey, strings.NewReader("outside"), PutOptions{}); err != nil {
		t.Fatalf("outside Put() error = %v", err)
	}
	walkPrefix := mustParseKey(t, prefix)
	var walked []string
	if err := store.Walk(ctx, walkPrefix, func(info ObjectInfo) error {
		walked = append(walked, info.Key.String())
		return nil
	}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	slices.Sort(walked)
	wantWalked := []string{siblingKey.String(), key.String()}
	slices.Sort(wantWalked)
	if !slices.Equal(walked, wantWalked) {
		t.Fatalf("Walk() = %v, want %v", walked, wantWalked)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() before reconnect error = %v", err)
	}
	reconnected, err := NewSFTPStore(ctx, options)
	if err != nil {
		t.Fatalf("NewSFTPStore() after reconnect error = %v", err)
	}
	cleanupStore = reconnected
	reopened, err := reconnected.Open(ctx, key)
	if err != nil {
		t.Fatalf("Open() after reconnect error = %v", err)
	}
	gotReplacement, readErr := io.ReadAll(reopened.Body)
	closeErr := reopened.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read after reconnect = %v, close = %v", readErr, closeErr)
	}
	if !bytes.Equal(gotReplacement, replacement) {
		t.Fatalf("object after replacement = %q, want %q", gotReplacement, replacement)
	}

	for _, deleteKey := range cleanupKeys {
		if err := reconnected.Delete(ctx, deleteKey); err != nil {
			t.Fatalf("Delete(%s) error = %v", deleteKey.String(), err)
		}
		if err := reconnected.Delete(ctx, deleteKey); err != nil {
			t.Fatalf("second Delete(%s) error = %v", deleteKey.String(), err)
		}
	}
}
