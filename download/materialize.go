package download

import (
	"context"
	"fmt"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
)

// MaterializeSelection resolves every selected rendition to a local directory
// so path-oriented assemblers such as FFmpeg remain independent of the
// durable storage adapter.
func MaterializeSelection(ctx context.Context, service *storage.Service, file *models.File, selection *Selection) (func() error, error) {
	return MaterializeSelectionWithProgress(ctx, service, file, selection, nil)
}

// MaterializeSelectionWithProgress reports byte-based progress while fetching
// remote tracks. Existing local paths do not produce transfer progress.
func MaterializeSelectionWithProgress(ctx context.Context, service *storage.Service, file *models.File, selection *Selection, progress func(float64)) (func() error, error) {
	if service == nil {
		return func() error { return nil }, nil
	}
	if file == nil || selection == nil || service.Layout() == nil {
		return nil, storage.ErrStoreNotConfigured
	}

	qualityPrefix, err := service.Layout().VideoPrefix(file.UUID, selection.Quality.Name)
	if err != nil {
		return nil, err
	}
	prefixes := []storage.PrefixMaterialization{{Prefix: qualityPrefix, Purpose: "download-quality"}}

	for index := range selection.Audios {
		audio := &selection.Audios[index]
		prefix, err := service.Layout().AudioPrefix(file.UUID, audio.UUID)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, storage.PrefixMaterialization{Prefix: prefix, Purpose: "download-audio"})
	}

	for index := range selection.Subtitles {
		subtitle := &selection.Subtitles[index]
		prefix, err := service.Layout().SubtitlePrefix(file.UUID, subtitle.UUID)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, storage.PrefixMaterialization{Prefix: prefix, Purpose: "download-subtitle"})
	}

	paths, cleanup, err := service.MaterializePrefixes(ctx, file.StorageID, prefixes, progress)
	if err != nil {
		return nil, fmt.Errorf("materialize selected tracks: %w", err)
	}
	selection.Quality.Path = paths[0]
	for index := range selection.Audios {
		selection.Audios[index].Path = paths[1+index]
	}
	for index := range selection.Subtitles {
		selection.Subtitles[index].Path = paths[1+len(selection.Audios)+index]
	}
	return cleanup, nil
}
