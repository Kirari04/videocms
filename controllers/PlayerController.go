package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
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
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink)
	if res.Error != nil {
		log.Print(res.Error)
		return c.Status(fiber.StatusNotFound).Render("404", fiber.Map{})
	}

	return c.Render("player", fiber.Map{
		"Title":    dbLink.File.Name,
		"Qualitys": dbLink.File.Qualitys,
		"UUID":     requestValidation.UUID,
	})
}
