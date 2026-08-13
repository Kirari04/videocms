package download

import (
	"context"
	"errors"
	"fmt"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
)

// MaterializeSelection resolves every selected rendition to a local directory
// so path-oriented assemblers such as FFmpeg remain independent of the
// durable storage adapter.
func MaterializeSelection(ctx context.Context, service *storage.Service, file *models.File, selection *Selection) (func() error, error) {
	if service == nil {
		return func() error { return nil }, nil
	}
	if file == nil || selection == nil || service.Layout() == nil {
		return nil, storage.ErrStoreNotConfigured
	}

	cleanups := make([]func() error, 0, 1+len(selection.Audios)+len(selection.Subtitles))
	cleanupAll := func() error {
		var cleanupErr error
		for index := len(cleanups) - 1; index >= 0; index-- {
			cleanupErr = errors.Join(cleanupErr, cleanups[index]())
		}
		return cleanupErr
	}
	fail := func(err error) (func() error, error) {
		_ = cleanupAll()
		return nil, err
	}

	qualityPrefix, err := service.Layout().VideoPrefix(file.UUID, selection.Quality.Name)
	if err != nil {
		return fail(err)
	}
	qualityPath, cleanup, err := service.MaterializePrefix(ctx, file.StorageID, qualityPrefix, "download-quality")
	if err != nil {
		return fail(fmt.Errorf("materialize quality %s: %w", selection.Quality.Name, err))
	}
	cleanups = append(cleanups, cleanup)
	selection.Quality.Path = qualityPath

	for index := range selection.Audios {
		audio := &selection.Audios[index]
		prefix, err := service.Layout().AudioPrefix(file.UUID, audio.UUID)
		if err != nil {
			return fail(err)
		}
		localPath, cleanup, err := service.MaterializePrefix(ctx, file.StorageID, prefix, "download-audio")
		if err != nil {
			return fail(fmt.Errorf("materialize audio %s: %w", audio.UUID, err))
		}
		cleanups = append(cleanups, cleanup)
		audio.Path = localPath
	}

	for index := range selection.Subtitles {
		subtitle := &selection.Subtitles[index]
		prefix, err := service.Layout().SubtitlePrefix(file.UUID, subtitle.UUID)
		if err != nil {
			return fail(err)
		}
		localPath, cleanup, err := service.MaterializePrefix(ctx, file.StorageID, prefix, "download-subtitle")
		if err != nil {
			return fail(fmt.Errorf("materialize subtitle %s: %w", subtitle.UUID, err))
		}
		cleanups = append(cleanups, cleanup)
		subtitle.Path = localPath
	}

	return cleanupAll, nil
}
