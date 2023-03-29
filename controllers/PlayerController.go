package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	res := inits.DB.
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink)
	if res.Error != nil {
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
	}

	//check if has some file is ready
	var streamIsReady bool
	var jsonQualitys []map[string]string
	for _, qualiItem := range dbLink.File.Qualitys {
		if qualiItem.Ready {
			streamIsReady = true
			if qualiItem.Type != "hls" {
				jsonQualitys = append(jsonQualitys, map[string]string{
					"url":    fmt.Sprintf("/videos/qualitys/%s/%s/%s", dbLink.UUID, qualiItem.Name, qualiItem.OutputFile),
					"label":  qualiItem.Name,
					"height": strconv.Itoa(int(qualiItem.Height)),
					"width":  strconv.Itoa(int(qualiItem.Width)),
				})
			}
		}
	}
	type OgQualityT struct {
		Url    string
		Label  string
		Height string
		Width  string
	}
	var OgQuality OgQualityT
	if len(jsonQualitys) > 0 {
		OgQuality = OgQualityT{
			Url:    jsonQualitys[0]["url"],
			Label:  jsonQualitys[0]["label"],
			Height: jsonQualitys[0]["height"],
			Width:  jsonQualitys[0]["width"],
		}
	}

	rawQuality, _ := json.Marshal(jsonQualitys)

	var jsonSubtitles []map[string]string
	for _, subItem := range dbLink.File.Subtitles {
		if subItem.Ready {
			subPath := fmt.Sprintf("./videos/qualitys/%s/%s/%s", dbLink.File.UUID, subItem.UUID, subItem.OutputFile)
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

	var jsonAudios []map[string]string
	for _, audioItem := range dbLink.File.Audios {
		if audioItem.Ready {
			jsonAudios = append(jsonAudios, map[string]string{
				"uuid": audioItem.UUID,
				"type": audioItem.Type,
				"name": audioItem.Name,
				"lang": audioItem.Lang,
			})
		}
	}
	rawAudios, _ := json.Marshal(jsonAudios)

	return c.Render("player", fiber.Map{
		"Title":         dbLink.Name,
		"Thumbnail":     fmt.Sprintf("/videos/qualitys/%s/image/thumb/%s", dbLink.UUID, dbLink.File.Thumbnail),
		"Qualitys":      string(rawQuality),
		"OgQuality":     OgQuality,
		"Subtitles":     string(rawSubtitles),
		"Audios":        string(rawAudios),
		"StreamIsReady": streamIsReady,
		"UUID":          requestValidation.UUID,
		"PROJECTURL":    config.ENV.Project,
	})
}
