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

	//check if requested link exists
	var currentLink models.Link
	if res := inits.DB.First(&currentLink, fileValidation.LinkID); res.Error != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "LinkID",
				Tag:         "exists",
				Value:       "File doesn't exist",
			},
		})
	}

	// delete link
	if res := inits.DB.Delete(&models.Link{}, fileValidation.LinkID); res.Error != nil {
		log.Printf("Failed to delete link: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	//check if any links left, else (=0) delete original file too
	var countLinks int64
	if res := inits.DB.
		Model(&models.Link{}).
		Where(&models.Link{
			FileID: currentLink.FileID,
		}).
		Count(&countLinks); res.Error != nil {
		log.Printf("Failed to delete link: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if countLinks == 0 {
		// delete file
		if res := inits.DB.Delete(&models.File{}, currentLink.FileID); res.Error != nil {
			log.Printf("Failed to delete file: %v", res.Error)
			return c.SendStatus(fiber.StatusInternalServerError)
		}
	}

	return c.SendStatus(fiber.StatusOK)
}
