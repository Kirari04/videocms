package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func DeleteFolder(c *fiber.Ctx) error {
	// parse & validate request
	var folderValidation models.FolderDeleteValidation
	if err := c.BodyParser(&folderValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")

	}
	if errors := helpers.ValidateStruct(folderValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	// Business logic
	status, err := logic.DeleteFolders(&models.FoldersDeleteValidation{
		FolderIDs: []models.FolderDeleteValidation{
			folderValidation,
		},
	}, c.Locals("UserID").(uint))
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}
	return c.SendStatus(status)
}
