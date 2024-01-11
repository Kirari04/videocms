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

func UpdateWebPage(c *fiber.Ctx) error {
	// parse & validate request
	var validatus models.WebPageUpdateValidation
	if err := c.BodyParser(&validatus); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validatus); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}
	var existing int64
	if res := inits.DB.Model(&models.WebPage{}).
		Where("id != ?", validatus.WebPageID).
		Where("path = ?", validatus.Path).
		Count(&existing); res.Error != nil {
		log.Println("Failed to count webpage path", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if existing > 0 {
		return c.Status(fiber.StatusBadRequest).SendString("Path already used")
	}

	var webPage models.WebPage
	if res := inits.DB.First(&webPage, validatus.WebPageID); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).SendString("Webpage not found")
		}
		log.Println("Failed to find webpage", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	webPage.Path = validatus.Path
	webPage.Title = validatus.Title
	webPage.Html = validatus.Html
	webPage.ListInFooter = *validatus.ListInFooter

	if res := inits.DB.Save(&webPage); res.Error != nil {
		log.Println("Failed to update webpage", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).SendString("ok")
}
