package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func CloneFile(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.FileCloneValidation
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

	//check if requested folder exists (if set)
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

	// check file hash with database
	var existingFile models.File
	if res := inits.DB.
		Where(&models.File{
			Hash: fileValidation.Sha256,
		}).First(&existingFile); res.Error != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}

	// file is dublicate and can be linked
	// link old uploaded file to new link
	dbLink := models.Link{
		UUID:           uuid.NewString(),
		ParentFolderID: fileValidation.ParentFolderID,
		UserID:         c.Locals("UserID").(uint),
		FileID:         existingFile.ID,
		Name:           fileValidation.Name,
	}
	if res := inits.DB.Create(&dbLink); res.Error != nil {
		log.Printf("Error saving link in database: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(dbLink)
}
