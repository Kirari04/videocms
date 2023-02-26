package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"

	"github.com/gofiber/fiber/v2"
)

func DeleteFolder(c *fiber.Ctx) error {
	// parse & validate request
	var folderValidation models.FolderDeleteValidation
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

	//check if requested folder exists
	res := inits.DB.First(&models.Folder{}, folderValidation.FolderID)
	if res.Error != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FolderID",
				Tag:         "exists",
				Value:       "Parent folder doesn't exist",
			},
		})
	}

	// query all containing
	deletedItems := 0
	userID := c.Locals("UserID").(uint)
	if err := deleteRecursive(&deletedItems, &folderValidation.FolderID, &userID); err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// delete top if is not 0
	if folderValidation.FolderID > 0 {
		inits.DB.Delete(&models.Folder{}, folderValidation.FolderID)
		deletedItems += 1
	}

	if deletedItems == 0 {
		return c.SendStatus(fiber.StatusNotModified)
	}

	return c.SendStatus(200)
}

func deleteRecursive(DeletedItemsptr *int, ParentFolderIDptr *uint, UserIDptr *uint) error {
	var folders []models.Folder

	if res := inits.DB.
		Model(&models.Folder{}).
		Preload("User").
		Where(&models.Folder{
			ParentFolderID: *ParentFolderIDptr,
			UserID:         *UserIDptr,
		}, "ParentFolderID", "UserID").
		Find(&folders); res.Error != nil {
		return res.Error
	}

	for _, folder := range folders {
		if err := deleteRecursive(DeletedItemsptr, &folder.ID, UserIDptr); err != nil {
			return err
		}
		if res := inits.DB.Delete(&models.Folder{}, folder.ID); res.Error != nil {
			return res.Error
		}
		*DeletedItemsptr += 1
	}

	return nil
}
