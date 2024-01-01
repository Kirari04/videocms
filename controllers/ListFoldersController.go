package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func ListFolders(c *fiber.Ctx) error {
	// parse & validate request
	var folderValidation models.FolderListValidation
	if err := c.QueryParser(&folderValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(folderValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	//check if requested folder exists
	if folderValidation.ParentFolderID > 0 {
		res := inits.DB.First(&models.Folder{}, folderValidation.ParentFolderID)
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

	// query all folders
	var folders []models.Folder
	res := inits.DB.
		Model(&models.Folder{}).
		Preload("User").
		Where(&models.Folder{
			ParentFolderID: folderValidation.ParentFolderID,
			UserID:         c.Locals("UserID").(uint),
		}, "ParentFolderID", "UserID").
		Order("name ASC").
		Find(&folders)
	if res.Error != nil {
		log.Printf("Failed to query folder list: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// return value
	return c.JSON(folders)
}
