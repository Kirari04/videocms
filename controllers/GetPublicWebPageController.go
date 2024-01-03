package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func GetPublicWebPage(c *fiber.Ctx) error {
	// parse & validate request
	var validatus models.WebPageGetValidation
	if err := c.QueryParser(&validatus); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validatus); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	var webPage models.WebPage
	if res := inits.DB.
		Where(&models.WebPage{
			Path: validatus.Path,
		}).
		First(&webPage); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).SendString("Page not found")
		}
		log.Println("Failed to get webpage", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).SendString(webPage.Html)
}
