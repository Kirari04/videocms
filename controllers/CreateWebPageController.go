package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func CreateWebPage(c *fiber.Ctx) error {
	// parse & validate request
	var validatus models.WebPageCreateValidation
	if err := c.BodyParser(&validatus); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validatus); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}
	var existing int64
	if res := inits.DB.Model(&models.WebPage{}).Where(&models.WebPage{
		Path: validatus.Path,
	}).Count(&existing); res.Error != nil {
		log.Println("Failed to count webpage path", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if existing > 0 {
		return c.Status(fiber.StatusBadRequest).SendString("Path already used")
	}

	webPage := models.WebPage{
		Path:         validatus.Path,
		Title:        validatus.Title,
		Html:         validatus.Html,
		ListInFooter: *validatus.ListInFooter,
	}
	if res := inits.DB.Create(&webPage); res.Error != nil {
		log.Println("Failed to create webpage", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).SendString("ok")
}
