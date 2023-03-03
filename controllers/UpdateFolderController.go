package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

/*
This function shouldnt be runned in concurrency.
The reason for that is, if the user woulf start the process of delete for the root folder
and shortly after calls the delete method for the root folder too, it would try to list the
whole folder tree and delete all folders & files again. To prevent that there is an map variable
that should prevent the user from calling this method multiple times concurrently.
*/
func UpdateFolder(c *fiber.Ctx) error {
	// parse & validate request
	var folderValidation models.FolderUpdateValidation
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

	var dbFolder models.Folder
	//check if requested folder id exists
	if res := inits.DB.First(&dbFolder, folderValidation.FolderID); res.Error != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FolderID",
				Tag:         "exists",
				Value:       "Folder doesn't exist",
			},
		})
	}

	/*
		check if ParentfolderID aint root folder (=0)
		check if requested parent folder id exists
		TODO: also check if the new parent folder is not a child of current folder or the folder itself
	*/
	if folderValidation.ParentFolderID > 0 {
		if res := inits.DB.First(&models.Folder{}, folderValidation.ParentFolderID); res.Error != nil {
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "ParentFolderID",
					Tag:         "exists",
					Value:       "Parent folder doesn't exist",
				},
			})
		}
	}

	//update folder
	dbFolder.Name = folderValidation.Name
	dbFolder.ParentFolderID = folderValidation.ParentFolderID
	if res := inits.DB.Save(&dbFolder); res.Error != nil {
		log.Printf("Failed to update folder: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}
