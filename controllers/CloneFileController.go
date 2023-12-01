package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func CloneFile(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.FileCloneValidation
	if err := c.BodyParser(&fileValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	// business logic
	status, dbLink, err := logic.CloneFileByHash(fileValidation.Sha256, fileValidation.ParentFolderID, fileValidation.Name, c.Locals("UserID").(uint))
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(dbLink)
}
