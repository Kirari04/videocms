package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func DeleteFileController(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.LinkDeleteValidation
	if err := c.BodyParser(&fileValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}
	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	//check if requested file exists
	if res := inits.DB.First(&models.Link{}, fileValidation.LinkID); res.Error != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FileID",
				Tag:         "exists",
				Value:       "File doesn't exist",
			},
		})
	}

	// delete file
	if res := inits.DB.Delete(&models.Link{}, fileValidation.LinkID); res.Error != nil {
		log.Printf("Failed to delete link: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}
