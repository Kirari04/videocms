package download

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"path/filepath"
)

const (
	ContainerMKV = "mkv"
	ContainerMP4 = "mp4"
)

type Selection struct {
	Container     string
	MediaDuration float64
	Quality       models.Quality
	Audios        []models.Audio
	Subtitles     []models.Subtitle
}

func ResolveSelection(
	dbLink *models.Link,
	qualityName string,
	container string,
	streaming bool,
	customSelection bool,
	audioUUIDs []string,
	subtitleUUIDs []string,
) (*Selection, error) {
	selection := &Selection{
		Container:     container,
		MediaDuration: dbLink.File.Duration,
	}

	qualityFound := false
	for _, quality := range dbLink.File.Qualitys {
		if quality.Name == qualityName && quality.Ready {
			selection.Quality = quality
			qualityFound = true
			break
		}
	}
	if !qualityFound {
		return nil, errors.New("selected quality is not available")
	}

	readyAudios := make(map[string]models.Audio)
	orderedReadyAudios := make([]models.Audio, 0, len(dbLink.File.Audios))
	for _, audio := range dbLink.File.Audios {
		if !audio.Ready {
			continue
		}
		readyAudios[audio.UUID] = audio
		orderedReadyAudios = append(orderedReadyAudios, audio)
	}

	if streaming {
		if len(orderedReadyAudios) > 0 {
			selection.Audios = []models.Audio{orderedReadyAudios[0]}
		}
		return selection, nil
	}

	if !customSelection {
		return nil, errors.New("download selection is required")
	}

	if container == ContainerMP4 {
		if len(subtitleUUIDs) > 0 {
			return nil, errors.New("MP4 downloads do not support subtitle selection")
		}
		if len(audioUUIDs) != 1 {
			return nil, errors.New("MP4 downloads require exactly one audio track")
		}
		audio, exists := readyAudios[audioUUIDs[0]]
		if !exists {
			return nil, errors.New("selected audio track is not available")
		}
		selection.Audios = []models.Audio{audio}
		return selection, nil
	}

	if container != ContainerMKV {
		return nil, errors.New("unsupported download container")
	}

	audios, err := selectedReadyAudios(audioUUIDs, readyAudios)
	if err != nil {
		return nil, err
	}
	subtitles, err := selectedReadySubtitles(subtitleUUIDs, dbLink.File.Subtitles)
	if err != nil {
		return nil, err
	}
	selection.Audios = audios
	selection.Subtitles = subtitles
	return selection, nil
}

func selectedReadyAudios(uuids []string, ready map[string]models.Audio) ([]models.Audio, error) {
	selected := make([]models.Audio, 0, len(uuids))
	seen := map[string]bool{}
	for _, selectedUUID := range uuids {
		if seen[selectedUUID] {
			continue
		}
		audio, exists := ready[selectedUUID]
		if !exists {
			return nil, errors.New("selected audio track is not available")
		}
		seen[selectedUUID] = true
		selected = append(selected, audio)
	}
	return selected, nil
}

func selectedReadySubtitles(uuids []string, subtitles []models.Subtitle) ([]models.Subtitle, error) {
	ready := make(map[string]models.Subtitle)
	for _, subtitle := range subtitles {
		if subtitle.Ready {
			ready[subtitle.UUID] = subtitle
		}
	}

	selected := make([]models.Subtitle, 0, len(uuids))
	seen := map[string]bool{}
	for _, selectedUUID := range uuids {
		if seen[selectedUUID] {
			continue
		}
		subtitle, exists := ready[selectedUUID]
		if !exists {
			return nil, errors.New("selected subtitle track is not available")
		}
		seen[selectedUUID] = true
		selected = append(selected, subtitle)
	}
	return selected, nil
}

func FFmpegArgs(selection *Selection, outputPath string, reportProgress bool) []string {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", filepath.Join(selection.Quality.Path, selection.Quality.OutputFile),
	}

	for _, audio := range selection.Audios {
		args = append(args, "-i", filepath.Join(audio.Path, audio.OutputFile))
	}
	for _, subtitle := range selection.Subtitles {
		args = append(args, "-i", filepath.Join(subtitle.Path, subtitle.OutputFile))
	}

	args = append(args, "-map", "0:v:0")
	inputIndex := 1
	for audioIndex, audio := range selection.Audios {
		args = append(args,
			"-map", fmt.Sprintf("%d:a:0", inputIndex),
			fmt.Sprintf("-metadata:s:a:%d", audioIndex), "language="+audio.Lang,
			fmt.Sprintf("-metadata:s:a:%d", audioIndex), "title="+audio.Name,
		)
		inputIndex++
	}
	for subtitleIndex, subtitle := range selection.Subtitles {
		args = append(args,
			"-map", fmt.Sprintf("%d:s:0", inputIndex),
			fmt.Sprintf("-metadata:s:s:%d", subtitleIndex), "language="+subtitle.Lang,
			fmt.Sprintf("-metadata:s:s:%d", subtitleIndex), "title="+subtitle.Name,
		)
		inputIndex++
	}

	args = append(args, "-c", "copy")
	if selection.Container == ContainerMP4 {
		args = append(args, "-movflags", "+faststart", "-f", "mp4")
	} else {
		args = append(args, "-f", "matroska")
	}
	if reportProgress {
		args = append(args, "-progress", "pipe:1", "-nostats")
	}
	return append(args, outputPath)
}
