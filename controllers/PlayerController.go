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

	var tkn string
	var streamIsReady bool
	var jsonQualitys []map[string]string
	var jsonSubtitles []map[string]string
	var jsonAudios []map[string]string
	var jsonWebhooks []map[string]any
	var streamUrl, streamUrlWidth, streamUrlHeight, firstAudio string

	// generate jwt token that allows the user to access the stream
	var err error
	tkn, _, err = auth.GenerateJWTStream(dbLink.UUID)
	if err != nil {
		log.Printf("Failed to generate jwt stream token: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	// List qualitys non hls & check if has some file is ready
	for _, qualiItem := range dbLink.File.Qualitys {
		if qualiItem.Ready {
			streamIsReady = true
			jsonQualitys = append(jsonQualitys, map[string]string{
				"url":    fmt.Sprintf("%s/%s/%s/download/video.mkv?jwt=%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, qualiItem.Name, tkn),
				"label":  qualiItem.Name,
				"height": strconv.Itoa(int(qualiItem.Height)),
				"width":  strconv.Itoa(int(qualiItem.Width)),
			})
			streamUrl = fmt.Sprintf("%s/%s/%s/%s/1/stream/video.mp4", config.ENV.FolderVideoQualitysPub, dbLink.UUID, qualiItem.Name, tkn)
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

	// "{{.UUID}}={{.JWT}}; path=/; domain=" + window.location.hostname + ";SameSite=None; Secure; HttpOnly"
	// c.SetCookie(&http.Cookie{
	// 	Name:     requestValidation.UUID,
	// 	Value:    tkn,
	// 	Path:     "/",
	// 	Secure:   true,
	// 	SameSite: "None",
	// 	Domain:   config.ENV.CookieDomain,
	// 	HTTPOnly: true,
	// })

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
		"PROJECTURL":                   config.ENV.Project,
		"Folder":                       config.ENV.FolderVideoQualitysPub,
		"JWT":                          tkn,
		"AppName":                      config.ENV.AppName,
		"BaseUrl":                      config.ENV.BaseUrl,
		"DownloadEnabled":              downloadsEnabled,
		"ContinueWatchingPopupEnabled": continueWatchingPopupEnabled,
	})
}
