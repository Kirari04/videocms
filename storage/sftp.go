package storage

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	randv2 "math/rand/v2"
	"mime"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	sftpHandshakeTimeout  = 15 * time.Second
	sftpKeepaliveInterval = 30 * time.Second
	sftpReadBufferSize    = 256 * 1024
	sftpTransferWorkers   = 8
)

type SFTPOptions struct {
	Host                 string
	Port                 int
	Username             string
	Root                 string
	Authentication       string
	HostKeyFingerprints  []string
	Password             string
	PrivateKey           string
	PrivateKeyPassphrase string
}

type SFTPStore struct {
	options SFTPOptions

	mu             sync.Mutex
	connection     *sftpConnection
	dialing        chan struct{}
	closed         bool
	nextGeneration uint64
	retryAt        time.Time
	dialFailures   int
}

type sftpConnection struct {
	ssh        *ssh.Client
	client     *sftp.Client
	root       string
	generation uint64
	done       chan struct{}
	closeOnce  sync.Once
}

func NewSFTPStore(ctx context.Context, options SFTPOptions) (*SFTPStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := normalizeSFTPOptions(options)
	if err != nil {
		return nil, err
	}
	store := &SFTPStore{options: normalized}
	connection, err := store.getConnection(ctx)
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, ErrStoreNotConfigured
	}
	return store, nil
}

func (s *SFTPStore) Open(ctx context.Context, key Key) (*Object, error) {
	validated, err := ParseKey(key.String())
	if err != nil {
		return nil, err
	}
	type openResult struct {
		file *sftp.File
		info ObjectInfo
	}
	result, connection, err := withSFTPConnection(ctx, s, func(connection *sftpConnection) (openResult, error) {
		remotePath, err := s.objectPath(connection, validated)
		if err != nil {
			return openResult{}, err
		}
		fileInfo, err := safeSFTPRegularFile(connection.client, connection.root, remotePath, validated)
		if err != nil {
			return openResult{}, err
		}
		file, err := connection.client.Open(remotePath)
		if err != nil {
			return openResult{}, normalizeSFTPError(validated, err)
		}
		return openResult{
			file: file,
			info: sftpObjectInfo(validated, fileInfo),
		}, nil
	}, func(connection *sftpConnection, result openResult) error {
		if result.file == nil {
			return nil
		}
		closeErr := result.file.Close()
		s.invalidate(connection, closeErr)
		return closeErr
	})
	if err != nil {
		return nil, err
	}
	return &Object{
		Body: &sftpReadSeekCloser{
			ctx:        ctx,
			store:      s,
			connection: connection,
			file:       result.file,
			size:       result.info.Size,
			reader:     bufio.NewReaderSize(result.file, sftpReadBufferSize),
		},
		Info: result.info,
	}, nil
}

func (s *SFTPStore) Put(ctx context.Context, key Key, src io.Reader, options PutOptions) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	if src == nil {
		return ObjectInfo{}, errors.New("SFTP upload source is nil")
	}
	validated, err := ParseKey(key.String())
	if err != nil {
		return ObjectInfo{}, err
	}
	if options.ExpectedSize != nil {
		if *options.ExpectedSize < 0 {
			return ObjectInfo{}, errors.New("SFTP expected upload size is negative")
		}
		if err := validateSeekableSize(src, *options.ExpectedSize); err != nil {
			return ObjectInfo{}, fmt.Errorf("object %s size mismatch: %w", validated.String(), err)
		}
	}

	connection, err := s.getConnection(ctx)
	if err != nil {
		return ObjectInfo{}, err
	}
	remotePath, err := s.objectPath(connection, validated)
	if err != nil {
		return ObjectInfo{}, err
	}
	parent := path.Dir(remotePath)
	if err := ensureSFTPDirectory(connection.client, connection.root, parent); err != nil {
		s.invalidate(connection, err)
		return ObjectInfo{}, fmt.Errorf("prepare SFTP object %s: %w", validated.String(), err)
	}
	if err := verifySFTPDirectory(connection.client, connection.root, parent); err != nil {
		s.invalidate(connection, err)
		return ObjectInfo{}, fmt.Errorf("verify SFTP object %s parent: %w", validated.String(), err)
	}
	if existing, statErr := connection.client.Lstat(remotePath); statErr == nil {
		if !existing.Mode().IsRegular() {
			return ObjectInfo{}, fmt.Errorf("SFTP object %s is not a regular file", validated.String())
		}
	} else if !isSFTPNotFound(statErr) {
		s.invalidate(connection, statErr)
		return ObjectInfo{}, normalizeSFTPError(validated, statErr)
	}

	temporaryPath, err := temporarySFTPPath(remotePath)
	if err != nil {
		return ObjectInfo{}, err
	}
	temporary, err := connection.client.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		s.invalidate(connection, err)
		return ObjectInfo{}, fmt.Errorf("create temporary SFTP object %s: %w", validated.String(), err)
	}
	temporaryExists := true
	defer func() {
		if temporaryExists {
			_ = connection.client.Remove(temporaryPath)
		}
	}()

	written, uploadErr := temporary.ReadFromWithConcurrency(&contextReader{ctx: ctx, reader: src}, sftpTransferWorkers)
	if uploadErr == nil && options.ExpectedSize != nil && written != *options.ExpectedSize {
		uploadErr = fmt.Errorf("object %s size mismatch: wrote %d, expected %d", validated.String(), written, *options.ExpectedSize)
	}
	if uploadErr == nil {
		if _, supported := connection.client.HasExtension("fsync@openssh.com"); supported {
			uploadErr = temporary.Sync()
		}
	}
	closeErr := temporary.Close()
	if uploadErr != nil || closeErr != nil {
		s.invalidate(connection, errors.Join(uploadErr, closeErr))
		return ObjectInfo{}, fmt.Errorf("upload SFTP object %s: %w", validated.String(), errors.Join(uploadErr, closeErr))
	}
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	temporaryInfo, err := connection.client.Lstat(temporaryPath)
	if err != nil {
		s.invalidate(connection, err)
		return ObjectInfo{}, fmt.Errorf("verify SFTP object %s: %w", validated.String(), err)
	}
	if !temporaryInfo.Mode().IsRegular() || temporaryInfo.Size() != written {
		return ObjectInfo{}, fmt.Errorf("verify SFTP object %s: temporary file size changed", validated.String())
	}
	if err := publishSFTPObject(connection.client, temporaryPath, remotePath); err != nil {
		s.invalidate(connection, err)
		return ObjectInfo{}, fmt.Errorf("publish SFTP object %s: %w", validated.String(), err)
	}
	temporaryExists = false
	fileInfo, err := safeSFTPRegularFile(connection.client, connection.root, remotePath, validated)
	if err != nil {
		s.invalidate(connection, err)
		return ObjectInfo{}, err
	}
	return sftpObjectInfo(validated, fileInfo), nil
}

func (s *SFTPStore) Stat(ctx context.Context, key Key) (ObjectInfo, error) {
	validated, err := ParseKey(key.String())
	if err != nil {
		return ObjectInfo{}, err
	}
	info, _, err := withSFTPConnection(ctx, s, func(connection *sftpConnection) (ObjectInfo, error) {
		remotePath, err := s.objectPath(connection, validated)
		if err != nil {
			return ObjectInfo{}, err
		}
		fileInfo, err := safeSFTPRegularFile(connection.client, connection.root, remotePath, validated)
		if err != nil {
			return ObjectInfo{}, err
		}
		return sftpObjectInfo(validated, fileInfo), nil
	}, nil)
	return info, err
}

func (s *SFTPStore) Delete(ctx context.Context, key Key) error {
	validated, err := ParseKey(key.String())
	if err != nil {
		return err
	}
	_, _, err = withSFTPConnection(ctx, s, func(connection *sftpConnection) (struct{}, error) {
		remotePath, err := s.objectPath(connection, validated)
		if err != nil {
			return struct{}{}, err
		}
		if _, err := safeSFTPRegularFile(connection.client, connection.root, remotePath, validated); err != nil {
			if errors.Is(err, ErrNotFound) {
				return struct{}{}, nil
			}
			return struct{}{}, err
		}
		if err := connection.client.Remove(remotePath); err != nil {
			if isSFTPNotFound(err) {
				return struct{}{}, nil
			}
			return struct{}{}, normalizeSFTPError(validated, err)
		}
		pruneSFTPDirectories(connection.client, connection.root, path.Dir(remotePath))
		return struct{}{}, nil
	}, nil)
	return err
}

func (s *SFTPStore) Walk(ctx context.Context, prefix Key, fn func(ObjectInfo) error) error {
	if fn == nil {
		return errors.New("storage walk callback is nil")
	}
	validated, err := ParseKey(prefix.String())
	if err != nil {
		return err
	}
	connection, err := s.getConnection(ctx)
	if err != nil {
		return err
	}
	remotePath, err := s.objectPath(connection, validated)
	if err != nil {
		return err
	}
	if err := verifySFTPDirectory(connection.client, connection.root, path.Dir(remotePath)); err != nil {
		if isSFTPNotFound(err) {
			return nil
		}
		s.invalidate(connection, err)
		return err
	}
	fileInfo, err := connection.client.Lstat(remotePath)
	if err != nil {
		if isSFTPNotFound(err) {
			return nil
		}
		s.invalidate(connection, err)
		return normalizeSFTPError(validated, err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to walk SFTP symbolic link %s", validated.String())
	}
	if fileInfo.Mode().IsRegular() {
		return fn(sftpObjectInfo(validated, fileInfo))
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("SFTP path %s is neither a file nor a directory", validated.String())
	}
	return s.walkDirectory(ctx, connection, remotePath, validated, fn)
}

func (s *SFTPStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	connection := s.connection
	s.connection = nil
	s.mu.Unlock()
	if connection == nil {
		return nil
	}
	return connection.close()
}

func (s *SFTPStore) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	suffix, err := randomSFTPSuffix()
	if err != nil {
		return err
	}
	key, err := ParseKey(".videocms-health-" + suffix)
	if err != nil {
		return err
	}
	const initialPayload = "videocms storage check"
	initialSize := int64(len(initialPayload))
	if _, err := s.Put(ctx, key, strings.NewReader(initialPayload), PutOptions{ExpectedSize: &initialSize}); err != nil {
		return fmt.Errorf("check SFTP write access: %w", err)
	}
	const expectedPayload = "videocms storage replacement check"
	replacementSize := int64(len(expectedPayload))
	if _, err := s.Put(ctx, key, strings.NewReader(expectedPayload), PutOptions{ExpectedSize: &replacementSize}); err != nil {
		_ = s.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("check SFTP safe replacement support: %w", err)
	}
	object, err := s.Open(ctx, key)
	if err != nil {
		_ = s.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("check SFTP read access: %w", err)
	}
	storedPayload, readErr := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil || string(storedPayload) != expectedPayload {
		_ = s.Delete(context.WithoutCancel(ctx), key)
		if readErr == nil && closeErr == nil {
			readErr = errors.New("SFTP storage check file content changed")
		}
		return fmt.Errorf("check SFTP read access: %w", errors.Join(readErr, closeErr))
	}
	if err := s.Delete(context.WithoutCancel(ctx), key); err != nil {
		return fmt.Errorf("remove SFTP storage check file: %w", err)
	}
	return nil
}

func (s *SFTPStore) walkDirectory(ctx context.Context, connection *sftpConnection, remoteDirectory string, logicalDirectory Key, fn func(ObjectInfo) error) error {
	if err := verifySFTPDirectory(connection.client, connection.root, remoteDirectory); err != nil {
		s.invalidate(connection, err)
		return fmt.Errorf("inspect SFTP directory %s: %w", logicalDirectory.String(), err)
	}
	entries, err := connection.client.ReadDirContext(ctx, remoteDirectory)
	if err != nil {
		s.invalidate(connection, err)
		return fmt.Errorf("list SFTP directory %s: %w", logicalDirectory.String(), err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Name() == "." || entry.Name() == ".." {
			continue
		}
		childKey, err := ParseKey(logicalDirectory.String() + "/" + entry.Name())
		if err != nil {
			return fmt.Errorf("invalid SFTP object name below %s: %w", logicalDirectory.String(), err)
		}
		childPath, err := s.objectPath(connection, childKey)
		if err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to walk SFTP symbolic link %s", childKey.String())
		}
		if entry.IsDir() {
			if err := s.walkDirectory(ctx, connection, childPath, childKey, fn); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			continue
		}
		if err := fn(sftpObjectInfo(childKey, entry)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SFTPStore) getConnection(ctx context.Context) (*sftpConnection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, ErrStoreNotConfigured
		}
		if s.connection != nil {
			connection := s.connection
			s.mu.Unlock()
			return connection, nil
		}
		if s.dialing != nil {
			waiting := s.dialing
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-waiting:
				continue
			}
		}
		retryAt := s.retryAt
		if delay := time.Until(retryAt); !retryAt.IsZero() && delay > 0 {
			s.mu.Unlock()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return nil, ctx.Err()
			case <-timer.C:
				continue
			}
		}
		s.dialing = make(chan struct{})
		waiting := s.dialing
		s.nextGeneration++
		generation := s.nextGeneration
		s.mu.Unlock()

		connection, err := dialSFTPConnection(ctx, s.options, generation)
		s.mu.Lock()
		if err == nil && !s.closed {
			s.connection = connection
			s.dialFailures = 0
			s.retryAt = time.Time{}
		} else if err != nil {
			s.dialFailures++
			s.retryAt = time.Now().Add(sftpRetryDelay(s.dialFailures))
		}
		s.dialing = nil
		close(waiting)
		closed := s.closed
		s.mu.Unlock()
		if connection != nil && closed {
			_ = connection.close()
		}
		if err != nil {
			return nil, err
		}
		if closed {
			return nil, ErrStoreNotConfigured
		}
		go s.keepalive(connection)
		return connection, nil
	}
}

func dialSFTPConnection(ctx context.Context, options SFTPOptions, generation uint64) (*sftpConnection, error) {
	address := net.JoinHostPort(options.Host, fmt.Sprintf("%d", options.Port))
	dialer := net.Dialer{Timeout: sftpHandshakeTimeout, KeepAlive: sftpKeepaliveInterval}
	rawConnection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to SFTP server %s: %w", address, err)
	}
	closeRaw := true
	defer func() {
		if closeRaw {
			_ = rawConnection.Close()
		}
	}()

	deadline := time.Now().Add(sftpHandshakeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := rawConnection.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set SFTP handshake deadline: %w", err)
	}
	authMethods, err := sftpAuthMethods(options)
	if err != nil {
		return nil, err
	}
	pinnedFingerprints := make(map[string]struct{}, len(options.HostKeyFingerprints))
	for _, fingerprint := range options.HostKeyFingerprints {
		pinnedFingerprints[fingerprint] = struct{}{}
	}
	clientConfig := &ssh.ClientConfig{
		User: options.Username,
		Auth: authMethods,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fingerprint := ssh.FingerprintSHA256(key)
			if _, allowed := pinnedFingerprints[fingerprint]; !allowed {
				return fmt.Errorf("SFTP host key mismatch for %s (received %s)", hostname, fingerprint)
			}
			return nil
		},
		Timeout: sftpHandshakeTimeout,
	}

	handshakeDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = rawConnection.Close()
		case <-handshakeDone:
		}
	}()
	sshConnection, channels, requests, err := ssh.NewClientConn(rawConnection, address, clientConfig)
	close(handshakeDone)
	if err != nil {
		return nil, fmt.Errorf("authenticate with SFTP server %s: %w", address, err)
	}
	sshClient := ssh.NewClient(sshConnection, channels, requests)
	closeSSH := true
	defer func() {
		if closeSSH {
			_ = sshClient.Close()
		}
	}()
	sftpClient, err := sftp.NewClient(
		sshClient,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(sftpTransferWorkers),
	)
	if err != nil {
		return nil, fmt.Errorf("start SFTP subsystem on %s: %w", address, err)
	}
	canonicalRoot, err := sftpClient.RealPath(options.Root)
	if err != nil {
		_ = sftpClient.Close()
		return nil, fmt.Errorf("resolve SFTP root %q: %w", options.Root, err)
	}
	rootInfo, err := sftpClient.Lstat(canonicalRoot)
	if err != nil {
		_ = sftpClient.Close()
		return nil, fmt.Errorf("inspect SFTP root %q: %w", canonicalRoot, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		_ = sftpClient.Close()
		return nil, fmt.Errorf("SFTP root %q is not a directory", canonicalRoot)
	}
	if err := rawConnection.SetDeadline(time.Time{}); err != nil {
		_ = sftpClient.Close()
		return nil, fmt.Errorf("clear SFTP handshake deadline: %w", err)
	}
	closeRaw = false
	closeSSH = false
	return &sftpConnection{
		ssh: sshClient, client: sftpClient, root: path.Clean(canonicalRoot),
		generation: generation, done: make(chan struct{}),
	}, nil
}

func (s *SFTPStore) keepalive(connection *sftpConnection) {
	ticker := time.NewTicker(sftpKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-connection.done:
			return
		case <-ticker.C:
			_, _, err := connection.ssh.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				s.invalidate(connection, err)
				return
			}
		}
	}
}

func (s *SFTPStore) invalidate(connection *sftpConnection, err error) {
	if connection == nil || !isSFTPTransportError(err) {
		return
	}
	s.mu.Lock()
	if s.connection == connection && s.connection.generation == connection.generation {
		s.connection = nil
	}
	s.mu.Unlock()
	_ = connection.close()
}

func (c *sftpConnection) close() error {
	var result error
	c.closeOnce.Do(func() {
		close(c.done)
		result = errors.Join(c.client.Close(), c.ssh.Close())
	})
	return result
}

func withSFTPConnection[T any](ctx context.Context, store *SFTPStore, operation func(*sftpConnection) (T, error), discard func(*sftpConnection, T) error) (T, *sftpConnection, error) {
	var zero T
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, nil, err
		}
		connection, err := store.getConnection(ctx)
		if err != nil {
			return zero, nil, err
		}
		result, err := operation(connection)
		if err == nil {
			if contextErr := ctx.Err(); contextErr != nil {
				if discard == nil {
					return zero, nil, contextErr
				}
				return zero, nil, errors.Join(contextErr, discard(connection, result))
			}
			return result, connection, nil
		}
		store.invalidate(connection, err)
		if !isSFTPTransportError(err) || attempt == 1 {
			return zero, nil, err
		}
	}
	return zero, nil, ErrStoreNotConfigured
}

func (s *SFTPStore) objectPath(connection *sftpConnection, key Key) (string, error) {
	validated, err := ParseKey(key.String())
	if err != nil {
		return "", err
	}
	remotePath := path.Join(connection.root, validated.String())
	rootPrefix := connection.root
	if rootPrefix != "/" {
		rootPrefix += "/"
	}
	if remotePath == connection.root || !strings.HasPrefix(remotePath, rootPrefix) {
		return "", fmt.Errorf("SFTP object %s is outside configured root", validated.String())
	}
	return remotePath, nil
}

func safeSFTPRegularFile(client *sftp.Client, root, remotePath string, key Key) (os.FileInfo, error) {
	if err := verifySFTPDirectory(client, root, path.Dir(remotePath)); err != nil {
		return nil, normalizeSFTPError(key, err)
	}
	fileInfo, err := client.Lstat(remotePath)
	if err != nil {
		return nil, normalizeSFTPError(key, err)
	}
	if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("SFTP object %s is not a regular file", key.String())
	}
	return fileInfo, nil
}

func verifySFTPDirectory(client *sftp.Client, root, directory string) error {
	root = path.Clean(root)
	directory = path.Clean(directory)
	if directory == root {
		return nil
	}
	rootPrefix := root
	if rootPrefix != "/" {
		rootPrefix += "/"
	}
	if !strings.HasPrefix(directory, rootPrefix) {
		return errors.New("SFTP directory is outside configured root")
	}
	current := root
	for _, segment := range strings.Split(strings.TrimPrefix(directory, rootPrefix), "/") {
		current = path.Join(current, segment)
		info, err := client.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("SFTP path %q is not a safe directory", current)
		}
	}
	return nil
}

func ensureSFTPDirectory(client *sftp.Client, root, directory string) error {
	root = path.Clean(root)
	directory = path.Clean(directory)
	if directory == root {
		return nil
	}
	rootPrefix := root
	if rootPrefix != "/" {
		rootPrefix += "/"
	}
	if !strings.HasPrefix(directory, rootPrefix) {
		return errors.New("SFTP directory is outside configured root")
	}
	relative := strings.TrimPrefix(directory, rootPrefix)
	current := root
	for _, segment := range strings.Split(relative, "/") {
		current = path.Join(current, segment)
		info, err := client.Lstat(current)
		if isSFTPNotFound(err) {
			mkdirErr := client.Mkdir(current)
			info, err = client.Lstat(current)
			if mkdirErr != nil && err != nil {
				return mkdirErr
			}
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("SFTP path %q is not a safe directory", current)
		}
	}
	return nil
}

func publishSFTPObject(client *sftp.Client, temporaryPath, remotePath string) error {
	if _, supported := client.HasExtension("posix-rename@openssh.com"); supported {
		return client.PosixRename(temporaryPath, remotePath)
	}
	renameErr := client.Rename(temporaryPath, remotePath)
	if renameErr == nil {
		return nil
	}
	if existing, statErr := client.Lstat(remotePath); statErr == nil && existing.Mode().IsRegular() {
		return fmt.Errorf("SFTP server rejected safe replacement of an existing file; atomic overwrite support is required: %w", renameErr)
	}
	return renameErr
}

func pruneSFTPDirectories(client *sftp.Client, root, directory string) {
	for directory != root && directory != "." && directory != "/" {
		if err := client.RemoveDirectory(directory); err != nil {
			return
		}
		directory = path.Dir(directory)
	}
}

func temporarySFTPPath(remotePath string) (string, error) {
	suffix, err := randomSFTPSuffix()
	if err != nil {
		return "", fmt.Errorf("generate SFTP temporary file name: %w", err)
	}
	return path.Join(path.Dir(remotePath), "."+path.Base(remotePath)+".videocms-upload-"+suffix), nil
}

func randomSFTPSuffix() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func sftpAuthMethods(options SFTPOptions) ([]ssh.AuthMethod, error) {
	switch options.Authentication {
	case SFTPAuthenticationPassword:
		keyboardInteractive := ssh.KeyboardInteractive(func(_ string, _ string, questions []string, echo []bool) ([]string, error) {
			if len(questions) != 1 || len(echo) != 1 || echo[0] {
				return nil, errors.New("SFTP server requested an unsupported interactive authentication challenge")
			}
			return []string{options.Password}, nil
		})
		return []ssh.AuthMethod{ssh.Password(options.Password), keyboardInteractive}, nil
	case SFTPAuthenticationPrivateKey:
		var signer ssh.Signer
		var err error
		if options.PrivateKeyPassphrase == "" {
			signer, err = ssh.ParsePrivateKey([]byte(options.PrivateKey))
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(options.PrivateKey), []byte(options.PrivateKeyPassphrase))
		}
		if err != nil {
			return nil, fmt.Errorf("parse SFTP private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, errors.New("unsupported SFTP authentication method")
	}
}

func sftpRetryDelay(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	delay := 250 * time.Millisecond
	for i := 1; i < failures && delay < 5*time.Second; i++ {
		delay *= 2
	}
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	jitterRange := delay / 5
	return delay - jitterRange + time.Duration(randv2.Int64N(int64(2*jitterRange)+1))
}

func sftpObjectInfo(key Key, info os.FileInfo) ObjectInfo {
	contentType := mime.TypeByExtension(path.Ext(key.String()))
	return ObjectInfo{Key: key, Size: info.Size(), ModTime: info.ModTime(), ContentType: contentType}
}

func normalizeSFTPError(key Key, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if isSFTPNotFound(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, key.String())
	}
	return fmt.Errorf("SFTP object %s: %w", key.String(), err)
}

func isSFTPNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var statusError *sftp.StatusError
	return errors.As(err, &statusError) && statusError.FxCode() == sftp.ErrSSHFxNoSuchFile
}

func isSFTPTransportError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	var statusError *sftp.StatusError
	if errors.As(err, &statusError) {
		return statusError.FxCode() == sftp.ErrSSHFxConnectionLost || statusError.FxCode() == sftp.ErrSSHFxNoConnection
	}
	return false
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if err == nil {
		if contextErr := r.ctx.Err(); contextErr != nil {
			return n, contextErr
		}
	}
	return n, err
}

type sftpReadSeekCloser struct {
	mu         sync.Mutex
	ctx        context.Context
	store      *SFTPStore
	connection *sftpConnection
	file       *sftp.File
	reader     *bufio.Reader
	size       int64
	position   int64
	closed     bool
}

func (r *sftpReadSeekCloser) Read(buffer []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fsClosedError()
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.position >= r.size {
		return 0, io.EOF
	}
	n, err := r.reader.Read(buffer)
	r.position += int64(n)
	if err != nil {
		r.store.invalidate(r.connection, err)
	}
	return n, err
}

func (r *sftpReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, fsClosedError()
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = r.position + offset
	case io.SeekEnd:
		target = r.size + offset
	default:
		return 0, errors.New("invalid seek whence")
	}
	if target < 0 {
		return 0, errors.New("negative seek position")
	}
	if target == r.position {
		return target, nil
	}
	position, err := r.file.Seek(target, io.SeekStart)
	if err != nil {
		r.store.invalidate(r.connection, err)
		return 0, err
	}
	r.position = position
	r.reader.Reset(r.file)
	return position, nil
}

func (r *sftpReadSeekCloser) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.file.Close()
	r.store.invalidate(r.connection, err)
	return err
}

var _ Store = (*SFTPStore)(nil)
var _ HealthChecker = (*SFTPStore)(nil)
var _ ReadSeekCloser = (*sftpReadSeekCloser)(nil)
