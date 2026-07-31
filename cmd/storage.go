package cmd

import (
	"context"
	"errors"
	"strings"

	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/storage"
)

func newStorageService(ctx context.Context, cfg config.Config) (*storage.Service, error) {
	localMediaStore, err := storage.NewLocalStore(cfg.FolderVideoQualitysPriv)
	if err != nil {
		return nil, err
	}
	closeLocalOnError := true
	defer func() {
		if closeLocalOnError {
			_ = localMediaStore.Close()
		}
	}()

	workspace, err := storage.NewLocalWorkspace(cfg.StorageScratchDir)
	if err != nil {
		return nil, err
	}
	stores := map[string]storage.Store{"local": localMediaStore}
	if strings.TrimSpace(cfg.StorageS3Bucket) != "" {
		usePathStyle := cfg.StorageS3UsePathStyle != nil && *cfg.StorageS3UsePathStyle
		s3Store, err := storage.NewS3Store(ctx, storage.S3Options{
			Bucket:            cfg.StorageS3Bucket,
			Region:            cfg.StorageS3Region,
			Endpoint:          cfg.StorageS3Endpoint,
			Prefix:            cfg.StorageS3Prefix,
			AccessKeyID:       cfg.StorageS3AccessKeyID,
			SecretAccessKey:   cfg.StorageS3SecretAccessKey,
			SessionToken:      cfg.StorageS3SessionToken,
			UsePathStyle:      usePathStyle,
			UploadPartSize:    cfg.StorageS3UploadPartSize,
			UploadConcurrency: cfg.StorageS3UploadConcurrency,
		})
		if err != nil {
			return nil, err
		}
		stores["s3"] = s3Store
	}
	if cfg.StorageDefaultStore == "s3" && stores["s3"] == nil {
		return nil, errors.New("StorageS3Bucket is required when StorageDefaultStore is s3")
	}
	storageService, err := storage.NewServiceWithWorkspace(
		cfg.StorageDefaultStore,
		storage.LegacyMediaLayout{},
		workspace,
		stores,
	)
	if err != nil {
		return nil, err
	}
	closeLocalOnError = false
	return storageService, nil
}
