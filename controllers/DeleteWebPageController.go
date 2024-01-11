package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func DeleteWebPage(c *fiber.Ctx) error {
	// parse & validate request
	var validatus models.WebPageDeleteValidation
	if err := c.BodyParser(&validatus); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validatus); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	res := inits.DB.Delete(&models.WebPage{}, validatus.WebPageID)
	if res.Error != nil {
		log.Println("Failed to delete webpage", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if res.RowsAffected <= 0 {
		return c.Status(fiber.StatusBadRequest).SendString("Webpage not found")
	}

	return c.Status(fiber.StatusOK).SendString("ok")
}
