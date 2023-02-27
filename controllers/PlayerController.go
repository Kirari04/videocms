package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"encoding/json"
	"fmt"
	"log"

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
		// Where(&models.Quality{
		// 	Ready:  true,
		// 	Failed: false,
		// }).
		First(&dbLink)
	if res.Error != nil {
		log.Print(res.Error)
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
	}

	var jsonQualitys []map[string]string
	for _, qualiItem := range dbLink.File.Qualitys {
		jsonQualitys = append(jsonQualitys, map[string]string{
			"file":  fmt.Sprintf("%s/out.mp4", qualiItem.Path),
			"label": qualiItem.Name,
		})
	}
	rawQuality, _ := json.Marshal(jsonQualitys)

	var jsonSubtitles []map[string]string
	for _, subItem := range dbLink.File.Subtitles {
		jsonSubtitles = append(jsonSubtitles, map[string]string{
			"file":  fmt.Sprintf("%s/out.vtt", subItem.Path),
			"label": subItem.Name,
			"kind":  "captions",
		})
	}
	rawSubtitles, _ := json.Marshal(jsonSubtitles)

	return c.Render("player", fiber.Map{
		"Title":     dbLink.File.Name,
		"Qualitys":  string(rawQuality),
		"Subtitles": string(rawSubtitles),
		"UUID":      requestValidation.UUID,
	})
}
