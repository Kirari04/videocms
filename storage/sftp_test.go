package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestSFTPStoreObjectLifecycle(t *testing.T) {
	server := newTestSFTPServer(t)
	root := filepath.Join(t.TempDir(), "media")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewSFTPStore(context.Background(), server.passwordOptions(root))
	if err != nil {
		t.Fatalf("NewSFTPStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	checkContext, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	if err := store.Check(checkContext); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	key, err := ParseKey("videos/demo/clip.txt")
	if err != nil {
		t.Fatal(err)
	}
	payload := "0123456789"
	expectedSize := int64(len(payload))
	info, err := store.Put(context.Background(), key, strings.NewReader(payload), PutOptions{ExpectedSize: &expectedSize})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if info.Size != expectedSize || info.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("Put() info = %#v", info)
	}

	object, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer object.Body.Close()
	buffer := make([]byte, 4)
	if _, err := io.ReadFull(object.Body, buffer); err != nil || string(buffer) != "0123" {
		t.Fatalf("first read = %q, %v", buffer, err)
	}
	if position, err := object.Body.Seek(-2, io.SeekCurrent); err != nil || position != 2 {
		t.Fatalf("Seek() = %d, %v", position, err)
	}
	remainder, err := io.ReadAll(object.Body)
	if err != nil || string(remainder) != "23456789" {
		t.Fatalf("remaining read = %q, %v", remainder, err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	canceledContext, cancelUpload := context.WithCancel(context.Background())
	canceledSource := &cancelAfterFirstRead{reader: strings.NewReader(strings.Repeat("x", 1024)), cancel: cancelUpload}
	canceledSize := int64(1024)
	if _, err := store.Put(canceledContext, key, canceledSource, PutOptions{ExpectedSize: &canceledSize}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Put() error = %v, want context.Canceled", err)
	}
	unchanged, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open() after canceled replacement error = %v", err)
	}
	unchangedPayload, readErr := io.ReadAll(unchanged.Body)
	closeErr := unchanged.Body.Close()
	if readErr != nil || closeErr != nil || string(unchangedPayload) != payload {
		t.Fatalf("object after canceled replacement = %q, read %v, close %v", unchangedPayload, readErr, closeErr)
	}
	temporaryFiles, err := filepath.Glob(filepath.Join(root, "videos", "demo", ".clip.txt.videocms-upload-*"))
	if err != nil || len(temporaryFiles) != 0 {
		t.Fatalf("temporary files after canceled replacement = %v, %v", temporaryFiles, err)
	}

	replacement := "replacement"
	replacementSize := int64(len(replacement))
	if _, err := store.Put(context.Background(), key, strings.NewReader(replacement), PutOptions{ExpectedSize: &replacementSize}); err != nil {
		t.Fatalf("replacement Put() error = %v", err)
	}
	prefix, _ := ParseKey("videos")
	var walked []string
	if err := store.Walk(context.Background(), prefix, func(info ObjectInfo) error {
		walked = append(walked, info.Key.String())
		return nil
	}); err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
	if !slices.Equal(walked, []string{key.String()}) {
		t.Fatalf("Walk() keys = %v", walked)
	}

	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatalf("idempotent Delete() error = %v", err)
	}
	if _, err := store.Stat(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Stat() error = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(root, "videos")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty parent directory was not pruned: %v", err)
	}
}

func TestWithSFTPConnectionDiscardsSuccessfulResultAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	connection := &sftpConnection{}
	store := &SFTPStore{connection: connection}
	cleaned := false

	result, returnedConnection, err := withSFTPConnection(ctx, store, func(got *sftpConnection) (string, error) {
		if got != connection {
			t.Fatalf("operation connection = %p, want %p", got, connection)
		}
		cancel()
		return "remote-handle", nil
	}, func(got *sftpConnection, value string) error {
		if got != connection {
			t.Fatalf("discard connection = %p, want %p", got, connection)
		}
		if value != "remote-handle" {
			t.Fatalf("discard value = %q", value)
		}
		cleaned = true
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("withSFTPConnection() error = %v, want context.Canceled", err)
	}
	if result != "" || returnedConnection != nil {
		t.Fatalf("canceled result = %q, connection = %p", result, returnedConnection)
	}
	if !cleaned {
		t.Fatal("successful result was not discarded after cancellation")
	}
}

type cancelAfterFirstRead struct {
	reader io.Reader
	cancel context.CancelFunc
	done   bool
}

func (r *cancelAfterFirstRead) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	if !r.done {
		r.done = true
		r.cancel()
	}
	return n, err
}

func TestSFTPStoreRejectsSymlinkTraversal(t *testing.T) {
	server := newTestSFTPServer(t)
	root := filepath.Join(t.TempDir(), "media")
	outside := t.TempDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store, err := NewSFTPStore(context.Background(), server.passwordOptions(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	key, _ := ParseKey("escape/secret.txt")
	if _, err := store.Stat(context.Background(), key); err == nil || !strings.Contains(err.Error(), "safe directory") {
		t.Fatalf("Stat() error = %v, want unsafe directory rejection", err)
	}
}

func TestSFTPStorePinsHostKey(t *testing.T) {
	server := newTestSFTPServer(t)
	root := t.TempDir()
	options := server.passwordOptions(root)
	wrongPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := ssh.NewPublicKey(wrongPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	options.HostKeyFingerprints = []string{ssh.FingerprintSHA256(wrongKey)}
	if _, err := NewSFTPStore(context.Background(), options); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("NewSFTPStore() error = %v, want host key mismatch", err)
	}
}

func TestSFTPStoreAuthenticatesWithPrivateKey(t *testing.T) {
	server := newTestSFTPServer(t)
	root := t.TempDir()
	options := server.privateKeyOptions(root)
	store, err := NewSFTPStore(context.Background(), options)
	if err != nil {
		t.Fatalf("NewSFTPStore() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestSFTPStoreAuthenticatesWithEncryptedPrivateKey(t *testing.T) {
	server := newTestSFTPServer(t)
	store, err := NewSFTPStore(context.Background(), server.encryptedPrivateKeyOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("NewSFTPStore() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSFTPStoreAuthenticatesWithKeyboardInteractivePassword(t *testing.T) {
	server := newTestSFTPServerWithKeyboardInteractive(t)
	store, err := NewSFTPStore(context.Background(), server.passwordOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("NewSFTPStore() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewStoreFromSFTPMount(t *testing.T) {
	server := newTestSFTPServer(t)
	root := t.TempDir()
	options := server.passwordOptions(root)
	cipher, err := NewCredentialCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	configuration, credentials, err := EncodeSFTPMount(
		SFTPMountConfiguration{
			Host: options.Host, Port: options.Port, Username: options.Username, Root: root,
			Authentication: options.Authentication, HostKeyFingerprints: options.HostKeyFingerprints,
		},
		SFTPMountCredentials{Password: options.Password},
		"sftp-mount",
		cipher,
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStoreFromMount(context.Background(), MountProviderSFTP, "sftp-mount", configuration, credentials, cipher)
	if err != nil {
		t.Fatalf("NewStoreFromMount() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.(HealthChecker).Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestNormalizeSFTPOptionsRequiresSecureExplicitSettings(t *testing.T) {
	base := SFTPOptions{
		Host: "storage.example.com", Username: "media", Root: ".",
		Authentication: SFTPAuthenticationPassword, Password: "secret",
		HostKeyFingerprints: []string{"SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}
	normalized, err := normalizeSFTPOptions(base)
	if err != nil {
		t.Fatalf("normalizeSFTPOptions() error = %v", err)
	}
	if normalized.Port != 22 {
		t.Fatalf("Port = %d, want 22", normalized.Port)
	}

	tests := []struct {
		name   string
		mutate func(*SFTPOptions)
		want   string
	}{
		{name: "missing fingerprint", mutate: func(value *SFTPOptions) { value.HostKeyFingerprints = nil }, want: "fingerprint"},
		{name: "MD5 fingerprint", mutate: func(value *SFTPOptions) { value.HostKeyFingerprints = []string{"MD5:00"} }, want: "SHA256"},
		{name: "host includes port", mutate: func(value *SFTPOptions) { value.Host = "storage.example.com:22" }, want: "must not include a port"},
		{name: "unknown authentication", mutate: func(value *SFTPOptions) { value.Authentication = "agent" }, want: "authentication"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if _, err := normalizeSFTPOptions(value); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeSFTPOptions() error = %v, want %q", err, test.want)
			}
		})
	}
}

type testSFTPServer struct {
	t                       *testing.T
	listener                net.Listener
	hostSigner              ssh.Signer
	userSigner              ssh.Signer
	userKey                 ed25519.PrivateKey
	keyboardInteractiveOnly bool
	stop                    chan struct{}
	wait                    sync.WaitGroup
}

func newTestSFTPServer(t *testing.T) *testSFTPServer {
	return newTestSFTPServerWithAuthentication(t, false)
}

func newTestSFTPServerWithKeyboardInteractive(t *testing.T) *testSFTPServer {
	return newTestSFTPServerWithAuthentication(t, true)
}

func newTestSFTPServerWithAuthentication(t *testing.T, keyboardInteractiveOnly bool) *testSFTPServer {
	t.Helper()
	hostSigner, _ := newTestSSHSigner(t)
	userSigner, userKey := newTestSSHSigner(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testSFTPServer{
		t: t, listener: listener, hostSigner: hostSigner, userSigner: userSigner, userKey: userKey,
		keyboardInteractiveOnly: keyboardInteractiveOnly, stop: make(chan struct{}),
	}
	server.wait.Add(1)
	go server.serve()
	t.Cleanup(func() {
		close(server.stop)
		_ = server.listener.Close()
		server.wait.Wait()
	})
	return server
}

func newTestSSHSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer, privateKey
}

func (s *testSFTPServer) passwordOptions(root string) SFTPOptions {
	host, port, _ := net.SplitHostPort(s.listener.Addr().String())
	return SFTPOptions{
		Host: host, Port: mustTestPort(s.t, port), Username: "videocms", Root: root,
		Authentication: SFTPAuthenticationPassword, Password: "test-password",
		HostKeyFingerprints: []string{ssh.FingerprintSHA256(s.hostSigner.PublicKey())},
	}
}

func (s *testSFTPServer) privateKeyOptions(root string) SFTPOptions {
	options := s.passwordOptions(root)
	options.Authentication = SFTPAuthenticationPrivateKey
	options.Password = ""
	privateKey, err := ssh.MarshalPrivateKey(s.userKey, "test")
	if err != nil {
		s.t.Fatal(err)
	}
	options.PrivateKey = string(pem.EncodeToMemory(privateKey))
	return options
}

func (s *testSFTPServer) encryptedPrivateKeyOptions(root string) SFTPOptions {
	options := s.passwordOptions(root)
	options.Authentication = SFTPAuthenticationPrivateKey
	options.Password = ""
	options.PrivateKeyPassphrase = "key-passphrase"
	privateKey, err := ssh.MarshalPrivateKeyWithPassphrase(s.userKey, "test", []byte(options.PrivateKeyPassphrase))
	if err != nil {
		s.t.Fatal(err)
	}
	options.PrivateKey = string(pem.EncodeToMemory(privateKey))
	return options
}

func mustTestPort(t *testing.T, value string) int {
	t.Helper()
	address, err := net.ResolveTCPAddr("tcp", "127.0.0.1:"+value)
	if err != nil {
		t.Fatal(err)
	}
	return address.Port
}

func (s *testSFTPServer) serve() {
	defer s.wait.Done()
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				s.t.Errorf("accept SFTP test connection: %v", err)
				return
			}
		}
		s.wait.Add(1)
		go s.serveConnection(connection)
	}
}

func (s *testSFTPServer) serveConnection(connection net.Conn) {
	defer s.wait.Done()
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if metadata.User() == "videocms" && slices.Equal(key.Marshal(), s.userSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, errors.New("invalid public key")
		},
	}
	if s.keyboardInteractiveOnly {
		config.KeyboardInteractiveCallback = func(metadata ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("SFTP", "", []string{"Password:"}, []bool{false})
			if err == nil && metadata.User() == "videocms" && slices.Equal(answers, []string{"test-password"}) {
				return nil, nil
			}
			return nil, errors.New("invalid interactive credentials")
		}
	} else {
		config.PasswordCallback = func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if metadata.User() == "videocms" && string(password) == "test-password" {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		}
	}
	config.AddHostKey(s.hostSigner)
	_, channels, requests, err := ssh.NewServerConn(connection, config)
	if err != nil {
		_ = connection.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	for channelRequest := range channels {
		if channelRequest.ChannelType() != "session" {
			_ = channelRequest.Reject(ssh.UnknownChannelType, "session required")
			continue
		}
		channel, requests, err := channelRequest.Accept()
		if err != nil {
			continue
		}
		s.wait.Add(1)
		go func() {
			defer s.wait.Done()
			defer channel.Close()
			for request := range requests {
				var subsystem struct{ Name string }
				if request.Type != "subsystem" || ssh.Unmarshal(request.Payload, &subsystem) != nil || subsystem.Name != "sftp" {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				server, err := sftp.NewServer(channel)
				if err != nil {
					return
				}
				if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
					s.t.Errorf("serve SFTP subsystem: %v", err)
				}
				_ = server.Close()
				return
			}
		}()
	}
}
