package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path"
	"strings"

	"golang.org/x/crypto/ssh"
)

const (
	SFTPAuthenticationPassword   = "password"
	SFTPAuthenticationPrivateKey = "private_key"
	defaultSFTPPort              = 22
)

// SFTPMountConfiguration contains only non-secret connection settings. Host
// key fingerprints are required so a storage mount cannot silently connect to
// an impersonated server.
type SFTPMountConfiguration struct {
	Host                string   `json:"host"`
	Port                int      `json:"port"`
	Username            string   `json:"username"`
	Root                string   `json:"root"`
	Authentication      string   `json:"authentication"`
	HostKeyFingerprints []string `json:"host_key_fingerprints"`
}

type SFTPMountCredentials struct {
	Password             string `json:"password,omitempty"`
	PrivateKey           string `json:"private_key,omitempty"`
	PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty"`
}

func NewSFTPStoreFromMount(ctx context.Context, mountID, configuration, encryptedCredentials string, credentialCipher *CredentialCipher) (*SFTPStore, error) {
	config, err := DecodeSFTPMountConfiguration(configuration)
	if err != nil {
		return nil, fmt.Errorf("decode SFTP mount configuration: %w", err)
	}
	credentials := SFTPMountCredentials{}
	if encryptedCredentials != "" {
		if credentialCipher == nil {
			return nil, ErrEncryptionKeyNotConfigured
		}
		credentials, err = DecodeSFTPMountCredentials(encryptedCredentials, mountID, credentialCipher)
		if err != nil {
			return nil, err
		}
	}
	return NewSFTPStore(ctx, config.Options(credentials))
}

func (c SFTPMountConfiguration) Options(credentials SFTPMountCredentials) SFTPOptions {
	return SFTPOptions{
		Host:                 c.Host,
		Port:                 c.Port,
		Username:             c.Username,
		Root:                 c.Root,
		Authentication:       c.Authentication,
		HostKeyFingerprints:  append([]string(nil), c.HostKeyFingerprints...),
		Password:             credentials.Password,
		PrivateKey:           credentials.PrivateKey,
		PrivateKeyPassphrase: credentials.PrivateKeyPassphrase,
	}
}

func EncodeSFTPMount(configuration SFTPMountConfiguration, credentials SFTPMountCredentials, mountID string, credentialCipher *CredentialCipher) (string, string, error) {
	if credentialCipher == nil {
		return "", "", ErrEncryptionKeyNotConfigured
	}
	normalized, err := normalizeSFTPOptions(configuration.Options(credentials))
	if err != nil {
		return "", "", err
	}
	configuration = SFTPMountConfiguration{
		Host:                normalized.Host,
		Port:                normalized.Port,
		Username:            normalized.Username,
		Root:                normalized.Root,
		Authentication:      normalized.Authentication,
		HostKeyFingerprints: append([]string(nil), normalized.HostKeyFingerprints...),
	}
	credentials = SFTPMountCredentials{
		Password:             normalized.Password,
		PrivateKey:           normalized.PrivateKey,
		PrivateKeyPassphrase: normalized.PrivateKeyPassphrase,
	}
	encodedConfiguration, err := json.Marshal(configuration)
	if err != nil {
		return "", "", fmt.Errorf("encode SFTP mount configuration: %w", err)
	}
	encodedCredentials, err := json.Marshal(credentials)
	if err != nil {
		return "", "", fmt.Errorf("encode SFTP mount credentials: %w", err)
	}
	encryptedCredentials, err := credentialCipher.Encrypt(encodedCredentials, mountID)
	if err != nil {
		return "", "", err
	}
	return string(encodedConfiguration), encryptedCredentials, nil
}

func DecodeSFTPMountConfiguration(value string) (SFTPMountConfiguration, error) {
	var configuration SFTPMountConfiguration
	if err := json.Unmarshal([]byte(value), &configuration); err != nil {
		return SFTPMountConfiguration{}, err
	}
	return configuration, nil
}

func DecodeSFTPMountCredentials(value, mountID string, credentialCipher *CredentialCipher) (SFTPMountCredentials, error) {
	if credentialCipher == nil {
		return SFTPMountCredentials{}, ErrEncryptionKeyNotConfigured
	}
	plaintext, err := credentialCipher.Decrypt(value, mountID)
	if err != nil {
		return SFTPMountCredentials{}, err
	}
	var credentials SFTPMountCredentials
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return SFTPMountCredentials{}, fmt.Errorf("decode SFTP mount credentials: %w", err)
	}
	return credentials, nil
}

func normalizeSFTPOptions(options SFTPOptions) (SFTPOptions, error) {
	var err error
	options.Host, options.Port, err = normalizeSFTPAddress(options.Host, options.Port)
	if err != nil {
		return SFTPOptions{}, err
	}
	options.Username = strings.TrimSpace(options.Username)
	if options.Username == "" || strings.ContainsAny(options.Username, "\x00\r\n") {
		return SFTPOptions{}, errors.New("SFTP username is required")
	}
	options.Root = strings.TrimSpace(options.Root)
	if options.Root == "" {
		options.Root = "."
	}
	if strings.ContainsRune(options.Root, 0) || strings.Contains(options.Root, `\`) {
		return SFTPOptions{}, errors.New("SFTP root must be a POSIX path")
	}
	options.Root = path.Clean(options.Root)
	options.Authentication = strings.TrimSpace(options.Authentication)
	switch options.Authentication {
	case SFTPAuthenticationPassword:
		if options.Password == "" {
			return SFTPOptions{}, errors.New("SFTP password is required")
		}
		options.PrivateKey = ""
		options.PrivateKeyPassphrase = ""
	case SFTPAuthenticationPrivateKey:
		if strings.TrimSpace(options.PrivateKey) == "" {
			return SFTPOptions{}, errors.New("SFTP private key is required")
		}
		var signer ssh.Signer
		var err error
		if options.PrivateKeyPassphrase == "" {
			signer, err = ssh.ParsePrivateKey([]byte(options.PrivateKey))
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(options.PrivateKey), []byte(options.PrivateKeyPassphrase))
		}
		if err != nil || signer == nil {
			return SFTPOptions{}, fmt.Errorf("parse SFTP private key: %w", err)
		}
		options.Password = ""
	default:
		return SFTPOptions{}, errors.New("SFTP authentication must be password or private_key")
	}

	seen := make(map[string]struct{}, len(options.HostKeyFingerprints))
	fingerprints := make([]string, 0, len(options.HostKeyFingerprints))
	for _, fingerprint := range options.HostKeyFingerprints {
		fingerprint = strings.TrimSpace(fingerprint)
		if !strings.HasPrefix(fingerprint, "SHA256:") {
			return SFTPOptions{}, fmt.Errorf("invalid SFTP host key fingerprint %q: expected SHA256", fingerprint)
		}
		digest, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(fingerprint, "SHA256:"))
		if err != nil || len(digest) != 32 {
			return SFTPOptions{}, fmt.Errorf("invalid SFTP host key fingerprint %q", fingerprint)
		}
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		seen[fingerprint] = struct{}{}
		fingerprints = append(fingerprints, fingerprint)
	}
	if len(fingerprints) == 0 {
		return SFTPOptions{}, errors.New("at least one SFTP SHA256 host key fingerprint is required")
	}
	options.HostKeyFingerprints = fingerprints
	return options, nil
}

func normalizeSFTPAddress(host string, port int) (string, int, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", 0, errors.New("SFTP host is empty")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if strings.ContainsAny(host, "\x00/\\ 	\r\n") || strings.Contains(host, "://") {
		return "", 0, fmt.Errorf("invalid SFTP host %q", host)
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", 0, errors.New("SFTP host must not include a port")
	}
	if port == 0 {
		port = defaultSFTPPort
	}
	if port < 1 || port > 65535 {
		return "", 0, errors.New("SFTP port must be between 1 and 65535")
	}
	return host, port, nil
}
