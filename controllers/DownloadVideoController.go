package controllers

import (
	downloadsvc "ch/kirari04/videocms/download"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	downloadContainerMKV = downloadsvc.ContainerMKV
	downloadContainerMP4 = downloadsvc.ContainerMP4
)

var downloadQualityPattern = regexp.MustCompile(`^([0-9]{3,4}p|(h264))$`)

type resolvedDownloadSelection = downloadsvc.Selection

func (h *Handlers) DownloadVideoController(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		QUALITY string `validate:"required,min=1,max=10" param:"QUALITY"`
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

	if requestValidation.Stream == nil || !*requestValidation.Stream {
		return c.String(http.StatusBadRequest, "unsupported progressive stream")
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

	selection, err := resolveDownloadSelection(
		&dbLink,
		requestValidation.QUALITY,
		downloadContainerMP4,
		true,
		false,
		nil,
		nil,
	)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	cleanupInputs, err := downloadsvc.MaterializeSelection(c.Request().Context(), h.Deps.Storage, &dbLink.File, selection)
	if err != nil {
		c.Logger().Errorf("Failed to materialize download inputs: %v", err)
		return c.String(http.StatusInternalServerError, "failed to prepare download inputs")
	}
	defer cleanupInputs()

	if h.Deps.Storage == nil || h.Deps.Storage.Workspace() == nil {
		return c.String(http.StatusInternalServerError, "download workspace is unavailable")
	}
	tmpOutput, cleanupOutput, err := h.Deps.Storage.Workspace().TempFile(
		c.Request().Context(),
		"stream-download",
		"."+selection.Container,
	)
	if err != nil {
		return c.String(http.StatusInternalServerError, "failed to prepare download output")
	}
	tmpFilePath := tmpOutput.Name()
	if err := tmpOutput.Close(); err != nil {
		_ = cleanupOutput()
		return c.String(http.StatusInternalServerError, "failed to prepare download output")
	}
	defer cleanupOutput()

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

	fileName := fmt.Sprintf("%s[%s].mp4", safeDownloadName(dbLink.Name), requestValidation.QUALITY)
	c.Response().Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(c.Response(), c.Request(), fileName, time.Now(), tmpFile)
	return nil
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
	return downloadsvc.ResolveSelection(
		dbLink,
		qualityName,
		container,
		streaming,
		customSelection,
		audioUUIDs,
		subtitleUUIDs,
	)
}

func downloadFFmpegArgs(selection *resolvedDownloadSelection, outputPath string) []string {
	return downloadsvc.FFmpegArgs(selection, outputPath, false)
}

func safeDownloadName(name string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(name, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		return "video"
	}
	return safe
}
