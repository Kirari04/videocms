package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

type DownloadQualityOption struct {
	Name       string
	Dimensions string
	Height     int64
}

type DownloadTrackOption struct {
	UUID string
	Name string
	Lang string
	Type string
}

func (h *Handlers) DownloadPageController(c echo.Context) error {
	type Request struct {
		UUID string `validate:"required,uuid_rfc4122" param:"UUID"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.Render(status, "error.html", echo.Map{
			"Title": "Download error",
			"Error": err.Error(),
		})
	}

	if !h.downloadsEnabled() {
		return c.Render(http.StatusForbidden, "error.html", echo.Map{
			"Title": "Downloads disabled",
			"Error": "Downloads are not enabled for this server.",
		})
	}

	dbLink, err := h.loadPlayerLink(requestValidation.UUID)
	if err != nil {
		return c.Render(http.StatusNotFound, "404.html", echo.Map{})
	}

	if !h.playerCaptchaAllowed(c) {
		return c.Redirect(
			http.StatusSeeOther,
			"/captcha/challenge?uuid="+dbLink.UUID+"&return=download",
		)
	}

	mediaToken, mediaExpiration, err := h.Auth.GenerateMediaToken(buildMediaClaims(dbLink))
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to generate media token")
	}
	c.SetCookie(h.mediaCookie(c, dbLink.UUID, mediaToken, mediaExpiration))

	qualities := downloadQualityOptions(dbLink.File.Qualitys)
	audios := downloadAudioOptions(dbLink.File.Audios)
	subtitles := downloadSubtitleOptions(dbLink.File.Subtitles)

	return c.Render(http.StatusOK, "download.html", echo.Map{
		"Title":        fmt.Sprintf("Download %s - %s", dbLink.Name, h.Config().AppName),
		"VideoTitle":   dbLink.Name,
		"Thumbnail":    h.Logic.ResolvedThumbnailURL(*dbLink),
		"Duration":     formatDownloadDuration(dbLink.File.Duration),
		"UUID":         dbLink.UUID,
		"AppName":      h.Config().AppName,
		"DownloadBase": strings.TrimRight(h.Config().FolderVideoQualitysPub, "/") + "/" + dbLink.UUID,
		"Qualities":    qualities,
		"Audios":       audios,
		"Subtitles":    subtitles,
		"CanDownload":  len(qualities) > 0,
		"CanUseMP4":    len(audios) > 0,
	})
}

func (h *Handlers) downloadsEnabled() bool {
	return h.Config().DownloadEnabled != nil && *h.Config().DownloadEnabled
}

func downloadQualityOptions(qualities []models.Quality) []DownloadQualityOption {
	options := make([]DownloadQualityOption, 0, len(qualities))
	for _, quality := range qualities {
		if quality.Type != "hls" || !quality.Ready {
			continue
		}
		options = append(options, DownloadQualityOption{
			Name:       quality.Name,
			Dimensions: fmt.Sprintf("%d × %d", quality.Width, quality.Height),
			Height:     quality.Height,
		})
	}
	sort.SliceStable(options, func(i, j int) bool {
		return options[i].Height > options[j].Height
	})
	return options
}

func downloadAudioOptions(audios []models.Audio) []DownloadTrackOption {
	ready := make([]models.Audio, 0, len(audios))
	for _, audio := range audios {
		if audio.Ready {
			ready = append(ready, audio)
		}
	}
	sort.SliceStable(ready, func(i, j int) bool {
		return ready[i].Index < ready[j].Index
	})

	options := make([]DownloadTrackOption, 0, len(ready))
	for _, audio := range ready {
		options = append(options, DownloadTrackOption{
			UUID: audio.UUID,
			Name: audio.Name,
			Lang: audio.Lang,
			Type: strings.ToUpper(audio.Codec),
		})
	}
	return options
}

func downloadSubtitleOptions(subtitles []models.Subtitle) []DownloadTrackOption {
	preferred := preferredReadySubtitles(subtitles)
	options := make([]DownloadTrackOption, 0, len(preferred))
	for _, subtitle := range preferred {
		options = append(options, DownloadTrackOption{
			UUID: subtitle.UUID,
			Name: subtitle.Name,
			Lang: subtitle.Lang,
			Type: strings.ToUpper(subtitle.Type),
		})
	}
	return options
}

func preferredReadySubtitles(subtitles []models.Subtitle) []models.Subtitle {
	byIndex := map[int]models.Subtitle{}
	for _, subtitle := range subtitles {
		if !subtitle.Ready {
			continue
		}
		current, exists := byIndex[subtitle.Index]
		if !exists || (current.Type != "ass" && subtitle.Type == "ass") {
			byIndex[subtitle.Index] = subtitle
		}
	}

	indexes := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	preferred := make([]models.Subtitle, 0, len(indexes))
	for _, index := range indexes {
		preferred = append(preferred, byIndex[index])
	}
	return preferred
}

func formatDownloadDuration(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	if seconds < 60 {
		return "< 1m"
	}
	totalMinutes := int(seconds) / 60
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
