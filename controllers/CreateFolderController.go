package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"

	"github.com/gofiber/fiber/v2"
)

func CreateFolder(c *fiber.Ctx) error {
	// parse & validate request

	var folderValidation models.FolderCreateValidation
	if err := c.BodyParser(&folderValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}

	if errors := helpers.ValidateStruct(folderValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	//check if requested folder exists (if set)
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

	// create folder
	folder := models.Folder{
		Name:           folderValidation.Name,
		ParentFolderID: folderValidation.ParentFolderID,
		UserID:         c.Locals("UserID").(uint),
	}
	res := inits.DB.Model(&models.Folder{}).Create(&folder)
	if res.Error != nil {
		return c.Status(404).JSON([]helpers.ValidationError{
			{
				FailedField: "username",
				Tag:         "none",
				Value:       "User not found",
			},
		})
	}

	//return created folder
	return c.JSON(folder)
}
