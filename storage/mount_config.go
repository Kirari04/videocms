package storage

import (
	"context"
	"encoding/json"
	"fmt"
)

type S3MountConfiguration struct {
	Bucket            string `json:"bucket"`
	Region            string `json:"region"`
	Endpoint          string `json:"endpoint,omitempty"`
	Prefix            string `json:"prefix,omitempty"`
	UsePathStyle      bool   `json:"use_path_style"`
	UploadPartSize    int64  `json:"upload_part_size"`
	UploadConcurrency int    `json:"upload_concurrency"`
}

type S3MountCredentials struct {
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
}

func NewS3StoreFromMount(ctx context.Context, mountID, configuration, encryptedCredentials string, credentialCipher *CredentialCipher) (*S3Store, error) {
	var config S3MountConfiguration
	if err := json.Unmarshal([]byte(configuration), &config); err != nil {
		return nil, fmt.Errorf("decode S3 mount configuration: %w", err)
	}
	credentials := S3MountCredentials{}
	if encryptedCredentials != "" {
		plaintext, err := credentialCipher.Decrypt(encryptedCredentials, mountID)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(plaintext, &credentials); err != nil {
			return nil, fmt.Errorf("decode S3 mount credentials: %w", err)
		}
	}
	return NewS3Store(ctx, config.Options(credentials))
}

func (c S3MountConfiguration) Options(credentials S3MountCredentials) S3Options {
	return S3Options{
		Bucket:            c.Bucket,
		Region:            c.Region,
		Endpoint:          c.Endpoint,
		Prefix:            c.Prefix,
		AccessKeyID:       credentials.AccessKeyID,
		SecretAccessKey:   credentials.SecretAccessKey,
		SessionToken:      credentials.SessionToken,
		UsePathStyle:      c.UsePathStyle,
		UploadPartSize:    c.UploadPartSize,
		UploadConcurrency: c.UploadConcurrency,
	}
}

func EncodeS3Mount(configuration S3MountConfiguration, credentials S3MountCredentials, mountID string, credentialCipher *CredentialCipher) (string, string, error) {
	normalized, err := normalizeS3Options(configuration.Options(credentials))
	if err != nil {
		return "", "", err
	}
	configuration = S3MountConfiguration{
		Bucket:            normalized.Bucket,
		Region:            normalized.Region,
		Endpoint:          normalized.Endpoint,
		Prefix:            normalized.Prefix,
		UsePathStyle:      normalized.UsePathStyle,
		UploadPartSize:    normalized.UploadPartSize,
		UploadConcurrency: normalized.UploadConcurrency,
	}
	encodedConfiguration, err := json.Marshal(configuration)
	if err != nil {
		return "", "", fmt.Errorf("encode S3 mount configuration: %w", err)
	}
	encodedCredentials, err := json.Marshal(credentials)
	if err != nil {
		return "", "", fmt.Errorf("encode S3 mount credentials: %w", err)
	}
	encryptedCredentials, err := credentialCipher.Encrypt(encodedCredentials, mountID)
	if err != nil {
		return "", "", err
	}
	return string(encodedConfiguration), encryptedCredentials, nil
}

func DecodeS3MountConfiguration(value string) (S3MountConfiguration, error) {
	var configuration S3MountConfiguration
	if err := json.Unmarshal([]byte(value), &configuration); err != nil {
		return S3MountConfiguration{}, err
	}
	return configuration, nil
}

func DecodeS3MountCredentials(value, mountID string, credentialCipher *CredentialCipher) (S3MountCredentials, error) {
	plaintext, err := credentialCipher.Decrypt(value, mountID)
	if err != nil {
		return S3MountCredentials{}, err
	}
	var credentials S3MountCredentials
	if err := json.Unmarshal(plaintext, &credentials); err != nil {
		return S3MountCredentials{}, fmt.Errorf("decode S3 mount credentials: %w", err)
	}
	return credentials, nil
}
