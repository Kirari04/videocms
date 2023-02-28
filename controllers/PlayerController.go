package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"encoding/json"
	"fmt"
	"log"
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
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink)
	if res.Error != nil {
		log.Print(res.Error)
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
	}

	//check if has some file is ready

	var jsonQualitys []map[string]string
	var jsonEncQualitys []map[string]string
	for _, qualiItem := range dbLink.File.Qualitys {
		if qualiItem.Ready {
			jsonQualitys = append(jsonQualitys, map[string]string{
				"file":   fmt.Sprintf("%s/%s", qualiItem.Path, qualiItem.OutputFile),
				"label":  qualiItem.Name,
				"height": strconv.Itoa(int(qualiItem.Height)),
				"width":  strconv.Itoa(int(qualiItem.Width)),
			})
		}
		if qualiItem.Encoding {
			jsonEncQualitys = append(jsonEncQualitys, map[string]string{
				"progress": fmt.Sprintf("%v", qualiItem.Progress),
				"label":    qualiItem.Name,
				"height":   strconv.Itoa(int(qualiItem.Height)),
				"width":    strconv.Itoa(int(qualiItem.Width)),
			})
		}

	}
	if len(jsonQualitys) == 0 {
		return c.Render("encoding", fiber.Map{
			"Title":    dbLink.File.Name,
			"Qualitys": dbLink.File.Qualitys,
		})
	}
	rawQuality, _ := json.Marshal(jsonQualitys)
	rawEncQualitys, _ := json.Marshal(jsonEncQualitys)

	var jsonSubtitles []map[string]string
	for _, subItem := range dbLink.File.Subtitles {
		if subItem.Ready {
			jsonSubtitles = append(jsonSubtitles, map[string]string{
				"url":  fmt.Sprintf("%s/out.vtt", subItem.Path),
				"name": subItem.Name,
				"lang": subItem.Lang,
			})
		}
	}
	rawSubtitles, _ := json.Marshal(jsonSubtitles)

	return c.Render("player", fiber.Map{
		"Title":       dbLink.File.Name,
		"Qualitys":    string(rawQuality),
		"Subtitles":   string(rawSubtitles),
		"EncQualitys": string(rawEncQualitys),
		"UUID":        requestValidation.UUID,
	})
}
