package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func ListFiles(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.FileListValidation
	if err := c.QueryParser(&fileValidation); err != nil {
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

	//check if requested folder exists
	if fileValidation.ParentFolderID > 0 {
		res := inits.DB.First(&models.Folder{}, fileValidation.ParentFolderID)
		if res.Error != nil {
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "ParentFolderID",
					Tag:         "exists",
					Value:       "Parent folder doesn't exist",
				},
			})
		}
	}

	// query all files
	var links []models.Link
	res := inits.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Where(&models.Link{
			ParentFolderID: fileValidation.ParentFolderID,
			UserID:         c.Locals("UserID").(uint),
		}, "ParentFolderID", "UserID").
		Find(&links)
	if res.Error != nil {
		log.Printf("Failed to query file list: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// return value
	return c.JSON(links)
}
