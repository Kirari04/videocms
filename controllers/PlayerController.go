package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

func PlayerController(c echo.Context) error {
	// parse & validate request
	type Request struct {
		UUID string `validate:"required,uuid_rfc4122" param:"UUID"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.Render(status, "error.html", echo.Map{
			"Title": "Player Error",
			"Error": err.Error(),
		})
	}

	//check if requested folder exists
	var dbLink models.Link
	if res := inits.DB.
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); res.Error != nil {
		return c.Render(http.StatusNotFound, "404.html", echo.Map{})
	}

	if *config.ENV.CaptchaPlayerEnabled {
		cookie, err := c.Cookie("captcha_bypass")
		if err != nil || !auth.VerifyCaptchaJWT(cookie.Value, c.RealIP()) {
			return c.Redirect(http.StatusSeeOther, "/captcha/challenge?uuid="+dbLink.UUID)
		}
	}

	var streamIsReady bool
	var jsonQualitys []map[string]string
	var jsonSubtitles []map[string]string
	var jsonAudios []map[string]string
	var jsonWebhooks []map[string]any
	var streamUrl, streamUrlWidth, streamUrlHeight, firstAudio string

	mediaToken, mediaExpiration, err := auth.GenerateMediaToken(buildMediaClaims(&dbLink))
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to generate media token")
	}
	c.SetCookie(mediaCookie(c, dbLink.UUID, mediaToken, mediaExpiration))

	// List qualitys non hls & check if has some file is ready
	for _, qualiItem := range dbLink.File.Qualitys {
		if qualiItem.Ready {
			streamIsReady = true
			jsonQualitys = append(jsonQualitys, map[string]string{
				"url":    fmt.Sprintf("%s/%s/%s/download/video.mkv", config.ENV.FolderVideoQualitysPub, dbLink.UUID, qualiItem.Name),
				"label":  qualiItem.Name,
				"height": strconv.Itoa(int(qualiItem.Height)),
				"width":  strconv.Itoa(int(qualiItem.Width)),
			})
			streamUrl = fmt.Sprintf("%s/%s/%s/1/stream/video.mp4", config.ENV.FolderVideoQualitysPub, dbLink.UUID, qualiItem.Name)
			streamUrlHeight = strconv.Itoa(int(qualiItem.Height))
			streamUrlWidth = strconv.Itoa(int(qualiItem.Width))
		}
	}

	// List subtitles
	for _, subItem := range dbLink.File.Subtitles {
		if subItem.Ready {
			subPath := fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPriv, dbLink.File.UUID, subItem.UUID, subItem.OutputFile)
			if subContent, err := os.ReadFile(subPath); err == nil {
				jsonSubtitles = append(jsonSubtitles, map[string]string{
					"data": base64.StdEncoding.EncodeToString(subContent),
					"type": subItem.Type,
					"name": subItem.Name,
					"lang": subItem.Lang,
				})
			}
		}
	}

	// List audios
	for _, audioItem := range dbLink.File.Audios {
		if audioItem.Ready {
			jsonAudios = append(jsonAudios, map[string]string{
				"uuid": audioItem.UUID,
				"type": audioItem.Type,
				"name": audioItem.Name,
				"lang": audioItem.Lang,
				"file": audioItem.OutputFile,
			})
			firstAudio = audioItem.UUID
		}
	}

	// List webhooks
	var webhooks []models.Webhook
	if res := inits.DB.
		Where(&models.Webhook{
			UserID: dbLink.UserID,
		}).
		Find(&webhooks); res.Error != nil {
		log.Printf("Failed to query webhooks of file owner: %v", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	for _, webhookItem := range webhooks {
		jsonWebhooks = append(jsonWebhooks, map[string]any{
			"url":      webhookItem.Url,
			"rpm":      webhookItem.Rpm,
			"reqQuery": webhookItem.ReqQuery,
			"resField": webhookItem.ResField,
		})
	}

	rawQuality, _ := json.Marshal(jsonQualitys)
	rawSubtitles, _ := json.Marshal(jsonSubtitles)
	rawAudios, _ := json.Marshal(jsonAudios)
	rawWebhooks, _ := json.Marshal(jsonWebhooks)

	var downloadsEnabled bool
	if config.ENV.DownloadEnabled != nil {
		downloadsEnabled = *config.ENV.DownloadEnabled
	}

	var continueWatchingPopupEnabled bool
	if config.ENV.ContinueWatchingPopupEnabled != nil {
		continueWatchingPopupEnabled = *config.ENV.ContinueWatchingPopupEnabled
	}

	playerTemplate := "player_v2.html"
	if config.ENV.PlayerV2Enabled != nil && !*config.ENV.PlayerV2Enabled {
		playerTemplate = "player.html"
	}

	return c.Render(http.StatusOK, playerTemplate, echo.Map{
		"Title":                        fmt.Sprintf("%s - %s", config.ENV.AppName, dbLink.Name),
		"Description":                  fmt.Sprintf("Watch %s on %s", dbLink.Name, config.ENV.AppName),
		"Thumbnail":                    fmt.Sprintf("%s/%s/image/thumb/%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, dbLink.File.Thumbnail),
		"StreamUrl":                    template.HTML(streamUrl),
		"StreamUrlWidth":               streamUrlWidth,
		"StreamUrlHeight":              streamUrlHeight,
		"Width":                        dbLink.File.Width,
		"Height":                       dbLink.File.Height,
		"Qualitys":                     string(rawQuality),
		"Subtitles":                    string(rawSubtitles),
		"Audios":                       string(rawAudios),
		"AudioUUID":                    firstAudio,
		"Webhooks":                     string(rawWebhooks),
		"StreamIsReady":                streamIsReady,
		"UUID":                         requestValidation.UUID,
		"Folder":                       config.ENV.FolderVideoQualitysPub,
		"AppName":                      config.ENV.AppName,
		"BaseUrl":                      config.ENV.BaseUrl,
		"DownloadEnabled":              downloadsEnabled,
		"ContinueWatchingPopupEnabled": continueWatchingPopupEnabled,
	})
}

func buildMediaClaims(dbLink *models.Link) auth.MediaClaims {
	qualityIDs := map[string]uint{}
	audioIDs := map[string]uint{}
	subtitleUUIDs := []string{}

	for _, quality := range dbLink.File.Qualitys {
		if quality.Ready {
			qualityIDs[quality.Name] = quality.ID
		}
	}
	for _, audio := range dbLink.File.Audios {
		if audio.Ready {
			audioIDs[audio.UUID] = audio.ID
		}
	}
	for _, subtitle := range dbLink.File.Subtitles {
		if subtitle.Ready {
			subtitleUUIDs = append(subtitleUUIDs, subtitle.UUID)
		}
	}

	return auth.MediaClaims{
		LinkUUID:      dbLink.UUID,
		FileUUID:      dbLink.File.UUID,
		UserID:        dbLink.UserID,
		FileID:        dbLink.FileID,
		QualityIDs:    qualityIDs,
		AudioIDs:      audioIDs,
		SubtitleUUIDs: subtitleUUIDs,
	}
}

func mediaCookie(c echo.Context, linkUUID string, token string, expiration time.Time) *http.Cookie {
	secure := requestIsHTTPS(c)
	sameSite := http.SameSiteLaxMode
	if secure {
		sameSite = http.SameSiteNoneMode
	}

	return &http.Cookie{
		Name:     auth.MediaCookieName,
		Value:    token,
		Path:     mediaCookiePath(linkUUID),
		Expires:  expiration,
		MaxAge:   int(auth.MediaTokenDuration.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	}
}

func mediaCookiePath(linkUUID string) string {
	return strings.TrimRight(config.ENV.FolderVideoQualitysPub, "/") + "/" + linkUUID
}

func requestIsHTTPS(c echo.Context) bool {
	if strings.HasPrefix(strings.ToLower(config.ENV.BaseUrl), "https://") {
		return true
	}
	req := c.Request()
	if req.TLS != nil {
		return true
	}
	return strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https")
}
