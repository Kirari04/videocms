package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const (
	downloadContainerMKV = "mkv"
	downloadContainerMP4 = "mp4"
)

var downloadQualityPattern = regexp.MustCompile(`^([0-9]{3,4}p|(h264))$`)

type resolvedDownloadSelection struct {
	Container string
	Quality   models.Quality
	Audios    []models.Audio
	Subtitles []models.Subtitle
}

func (h *Handlers) DownloadVideoController(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		QUALITY string `validate:"required,min=1,max=10" param:"QUALITY"`
		FILE    string `validate:"omitempty,min=1,max=20" param:"FILE"`
		Stream  *bool  `validate:"omitempty,boolean" param:"STREAM"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	if !downloadQualityPattern.MatchString(requestValidation.QUALITY) {
		return c.String(http.StatusBadRequest, "bad quality format")
	}
	if !h.downloadsEnabled() {
		return c.String(http.StatusBadRequest, "download disabled")
	}

	streaming := requestValidation.Stream != nil && *requestValidation.Stream
	container, err := requestedDownloadContainer(requestValidation.FILE, streaming)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	var dbLink models.Link
	if dbRes := h.Deps.DB.
		Preload("File").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Preload("File.Qualitys").
		Where(&models.Link{UUID: requestValidation.UUID}).
		First(&dbLink); dbRes.Error != nil {
		return c.String(http.StatusBadRequest, "video doesn't exist")
	}

	customSelection := c.QueryParam("selection") == "custom"
	selection, err := resolveDownloadSelection(
		&dbLink,
		requestValidation.QUALITY,
		container,
		streaming,
		customSelection,
		c.QueryParams()["audio"],
		c.QueryParams()["subtitle"],
	)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	tmpFilePath := filepath.Join(
		h.Config().FolderVideoUploadsPriv,
		fmt.Sprintf("%s-download.%s", uuid.NewString(), selection.Container),
	)
	defer os.Remove(tmpFilePath)

	cmdArgs := downloadFFmpegArgs(selection, tmpFilePath)
	cmd := exec.CommandContext(c.Request().Context(), "ffmpeg", cmdArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		if errors.Is(c.Request().Context().Err(), context.Canceled) {
			return nil
		}
		c.Logger().Errorf("Failed to assemble download: %v: %s", err, strings.TrimSpace(string(output)))
		return c.String(http.StatusInternalServerError, "failed to prepare download")
	}

	tmpFile, err := os.Open(tmpFilePath)
	if err != nil {
		c.Logger().Error("Failed to open assembled download", err)
		return c.String(http.StatusInternalServerError, "failed to open prepared download")
	}
	defer tmpFile.Close()

	fileInfo, err := tmpFile.Stat()
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to inspect prepared download")
	}
	h.Logic.TrackTraffic(
		dbLink.File.UserID,
		dbLink.FileID,
		selection.Quality.ID,
		0,
		uint64(fileInfo.Size()),
	)

	fileName := fmt.Sprintf(
		"%s[%s].%s",
		safeDownloadName(dbLink.Name),
		requestValidation.QUALITY,
		selection.Container,
	)

	if streaming {
		c.Response().Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(c.Response(), c.Request(), fileName, time.Now(), tmpFile)
		return nil
	}

	contentType := "video/x-matroska"
	if selection.Container == downloadContainerMP4 {
		contentType = "video/mp4"
	}
	c.Response().Header().Set(
		"Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, fileName),
	)
	c.Response().Header().Set("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))
	return c.Stream(http.StatusOK, contentType, tmpFile)
}

func requestedDownloadContainer(fileName string, streaming bool) (string, error) {
	if streaming {
		return downloadContainerMP4, nil
	}
	switch fileName {
	case "video.mkv":
		return downloadContainerMKV, nil
	case "video.mp4":
		return downloadContainerMP4, nil
	default:
		return "", errors.New("unsupported download container")
	}
}

func resolveDownloadSelection(
	dbLink *models.Link,
	qualityName string,
	container string,
	streaming bool,
	customSelection bool,
	audioUUIDs []string,
	subtitleUUIDs []string,
) (*resolvedDownloadSelection, error) {
	selection := &resolvedDownloadSelection{Container: container}

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

	if container == downloadContainerMP4 {
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

	if container != downloadContainerMKV {
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

func selectedReadyAudios(
	uuids []string,
	ready map[string]models.Audio,
) ([]models.Audio, error) {
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

func selectedReadySubtitles(
	uuids []string,
	subtitles []models.Subtitle,
) ([]models.Subtitle, error) {
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

func downloadFFmpegArgs(selection *resolvedDownloadSelection, outputPath string) []string {
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
	if selection.Container == downloadContainerMP4 {
		args = append(args, "-movflags", "+faststart", "-f", "mp4")
	} else {
		args = append(args, "-f", "matroska")
	}
	return append(args, outputPath)
}

func safeDownloadName(name string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(name, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "video"
	}
	return safe
}
