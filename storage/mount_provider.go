package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	MountProviderS3   = "s3"
	MountProviderSFTP = "sftp"
)

func NewStoreFromMount(ctx context.Context, provider, mountID, configuration, encryptedCredentials string, credentialCipher *CredentialCipher) (Store, error) {
	switch provider {
	case MountProviderS3:
		return NewS3StoreFromMount(ctx, mountID, configuration, encryptedCredentials, credentialCipher)
	case MountProviderSFTP:
		return NewSFTPStoreFromMount(ctx, mountID, configuration, encryptedCredentials, credentialCipher)
	default:
		return nil, fmt.Errorf("unsupported storage provider %q", provider)
	}
}

// EncodeMount validates and serializes provider-specific settings while
// keeping credentials encrypted and bound to the durable mount UUID. A nil
// credentials value preserves the currently encrypted credentials on update.
func EncodeMount(provider string, configuration json.RawMessage, credentials *json.RawMessage, currentEncryptedCredentials, mountID string, credentialCipher *CredentialCipher) (string, string, error) {
	switch provider {
	case MountProviderS3:
		var config S3MountConfiguration
		if err := decodeMountJSON(configuration, &config, "S3 configuration"); err != nil {
			return "", "", err
		}
		var secret S3MountCredentials
		if credentials == nil {
			if currentEncryptedCredentials != "" {
				var err error
				secret, err = DecodeS3MountCredentials(currentEncryptedCredentials, mountID, credentialCipher)
				if err != nil {
					return "", "", err
				}
			}
		} else if err := decodeMountJSON(*credentials, &secret, "S3 credentials"); err != nil {
			return "", "", err
		}
		return EncodeS3Mount(config, secret, mountID, credentialCipher)
	case MountProviderSFTP:
		var config SFTPMountConfiguration
		if err := decodeMountJSON(configuration, &config, "SFTP configuration"); err != nil {
			return "", "", err
		}
		var secret SFTPMountCredentials
		if credentials == nil {
			if currentEncryptedCredentials != "" {
				var err error
				secret, err = DecodeSFTPMountCredentials(currentEncryptedCredentials, mountID, credentialCipher)
				if err != nil {
					return "", "", err
				}
			}
		} else if err := decodeMountJSON(*credentials, &secret, "SFTP credentials"); err != nil {
			return "", "", err
		}
		return EncodeSFTPMount(config, secret, mountID, credentialCipher)
	default:
		return "", "", fmt.Errorf("unsupported storage provider %q", provider)
	}
}

func DecodeMountConfiguration(provider, value string) (any, error) {
	switch provider {
	case MountProviderS3:
		return DecodeS3MountConfiguration(value)
	case MountProviderSFTP:
		return DecodeSFTPMountConfiguration(value)
	default:
		return nil, fmt.Errorf("unsupported storage provider %q", provider)
	}
}

func SameMountLocation(provider, left, right string) (bool, error) {
	switch provider {
	case MountProviderS3:
		leftConfig, err := DecodeS3MountConfiguration(left)
		if err != nil {
			return false, err
		}
		rightConfig, err := DecodeS3MountConfiguration(right)
		if err != nil {
			return false, err
		}
		return leftConfig.Bucket == rightConfig.Bucket &&
			leftConfig.Region == rightConfig.Region &&
			leftConfig.Endpoint == rightConfig.Endpoint &&
			leftConfig.Prefix == rightConfig.Prefix &&
			leftConfig.UsePathStyle == rightConfig.UsePathStyle, nil
	case MountProviderSFTP:
		leftConfig, err := DecodeSFTPMountConfiguration(left)
		if err != nil {
			return false, err
		}
		rightConfig, err := DecodeSFTPMountConfiguration(right)
		if err != nil {
			return false, err
		}
		return leftConfig.Host == rightConfig.Host &&
			leftConfig.Port == rightConfig.Port &&
			leftConfig.Username == rightConfig.Username &&
			leftConfig.Root == rightConfig.Root, nil
	default:
		return false, fmt.Errorf("unsupported storage provider %q", provider)
	}
}

func decodeMountJSON(value json.RawMessage, target any, label string) error {
	if len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return fmt.Errorf("%s is required", label)
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("decode %s: multiple JSON values", label)
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	return nil
}
