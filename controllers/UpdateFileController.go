package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func UpdateFile(c *fiber.Ctx) error {
	// parse & validate request
	var linkValidation models.LinkUpdateValidation
	if err := c.BodyParser(&linkValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}
	if errors := helpers.ValidateStruct(linkValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	var dbLink models.Link
	//check if requested file /link id exists
	if res := inits.DB.First(&dbLink, linkValidation.LinkID); res.Error != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "LinkID",
				Tag:         "exists",
				Value:       "File doesn't exist",
			},
		})
	}

	if linkValidation.ParentFolderID > 0 {
		if res := inits.DB.First(&models.Folder{}, linkValidation.ParentFolderID); res.Error != nil {
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "ParentFolderID",
					Tag:         "exists",
					Value:       "Parent folder doesn't exist",
				},
			})
		}
	}

	//update link data
	dbLink.Name = linkValidation.Name
	dbLink.ParentFolderID = linkValidation.ParentFolderID
	if res := inits.DB.Save(&dbLink); res.Error != nil {
		log.Printf("Failed to update link: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}
