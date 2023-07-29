package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func CreateUploadSession(c *fiber.Ctx) error {
	// parse & validate request
	var validation models.UploadSessionValidation
	if err := c.BodyParser(&validation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	// business logic
	uploadSessionUUID := uuid.NewString()
	status, response, err := logic.CreateUploadSession(
		validation.ParentFolderID,
		validation.Name,
		uploadSessionUUID,
		validation.Size,
		c.Locals("UserID").(uint),
	)
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(response)
}
