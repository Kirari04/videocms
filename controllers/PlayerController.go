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
	"log"
	"os"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func PlayerController(c *fiber.Ctx) error {
	// parse & validate request
	type Request struct {
		UUID string `validate:"required,uuid_rfc4122"`
	}
	var requestValidation Request
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
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
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
	}

	// List qualitys non hls & check if has some file is ready
	var streamIsReady bool
	var jsonQualitys []map[string]string
	for _, qualiItem := range dbLink.File.Qualitys {
		if qualiItem.Ready {
			streamIsReady = true
			if qualiItem.Type != "hls" {
				jsonQualitys = append(jsonQualitys, map[string]string{
					"url":    fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, qualiItem.Name, qualiItem.OutputFile),
					"label":  qualiItem.Name,
					"height": strconv.Itoa(int(qualiItem.Height)),
					"width":  strconv.Itoa(int(qualiItem.Width)),
				})
			}
		}
	}
	rawQuality, _ := json.Marshal(jsonQualitys)

	// List subtitles
	var jsonSubtitles []map[string]string
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
	rawSubtitles, _ := json.Marshal(jsonSubtitles)

	// List audios
	var jsonAudios []map[string]string
	for _, audioItem := range dbLink.File.Audios {
		if audioItem.Ready {
			jsonAudios = append(jsonAudios, map[string]string{
				"uuid": audioItem.UUID,
				"type": audioItem.Type,
				"name": audioItem.Name,
				"lang": audioItem.Lang,
				"file": audioItem.OutputFile,
			})
		}
	}
	rawAudios, _ := json.Marshal(jsonAudios)

	// List webhooks
	var webhooks []models.Webhook
	if res := inits.DB.
		Where(&models.Webhook{
			UserID: dbLink.UserID,
		}).
		Find(&webhooks); res.Error != nil {
		log.Printf("Failed to query webhooks of file owner: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	var jsonWebhooks []map[string]any
	for _, webhookItem := range webhooks {
		jsonWebhooks = append(jsonWebhooks, map[string]any{
			"url":      webhookItem.Url,
			"rpm":      webhookItem.Rpm,
			"reqQuery": webhookItem.ReqQuery,
			"resField": webhookItem.ResField,
		})
	}
	rawWebhooks, _ := json.Marshal(jsonWebhooks)

	// generate jwt token that allows the user to access the stream
	tkn, _, err := auth.GenerateJWTStream(dbLink.UUID)
	if err != nil {
		log.Printf("Failed to generate jwt stream token: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	// "{{.UUID}}={{.JWT}}; path=/; domain=" + window.location.hostname + ";SameSite=None; Secure; HttpOnly"
	c.Cookie(&fiber.Cookie{
		Name:        requestValidation.UUID,
		Value:       tkn,
		Path:        "/",
		Secure:      true,
		SameSite:    "none",
		HTTPOnly:    false,
		SessionOnly: true,
	})
	return c.Render("player", fiber.Map{
		"Title":         fmt.Sprintf("%s - %s", config.ENV.AppName, dbLink.Name),
		"Description":   fmt.Sprintf("Watch %s on %s", dbLink.Name, config.ENV.AppName),
		"Thumbnail":     fmt.Sprintf("%s/%s/image/thumb/%s", config.ENV.FolderVideoQualitysPub, dbLink.UUID, dbLink.File.Thumbnail),
		"Width":         dbLink.File.Width,
		"Height":        dbLink.File.Height,
		"Qualitys":      string(rawQuality),
		"Subtitles":     string(rawSubtitles),
		"Audios":        string(rawAudios),
		"Webhooks":      string(rawWebhooks),
		"StreamIsReady": streamIsReady,
		"UUID":          requestValidation.UUID,
		"PROJECTURL":    config.ENV.Project,
		"Folder":        config.ENV.FolderVideoQualitysPub,
		"JWT":           tkn,
	})
}
