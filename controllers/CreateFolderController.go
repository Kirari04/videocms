package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func CreateFolder(c *fiber.Ctx) error {
	// parse & validate request

	var folderValidation models.FolderCreateValidation
	if err := c.BodyParser(&folderValidation); err != nil {
		return c.Status(400).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(folderValidation); len(errors) > 0 {
		return c.Status(400).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	status, dbFolder, err := logic.CreateFolder(folderValidation.Name, folderValidation.ParentFolderID, c.Locals("UserID").(uint))

	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(dbFolder)
}
