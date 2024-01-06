package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func DeleteUploadSession(c *fiber.Ctx) error {
	// parse & validate request
	var validation models.DeleteUploadSessionValidation
	if err := c.BodyParser(&validation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	userId, ok := c.Locals("UserID").(uint)
	if !ok {
		log.Println("GetUploadSessions: Failed to catch userId")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	var uploadSession models.UploadSession
	if res := inits.DB.Where(&models.UploadSession{
		UUID: validation.UploadSessionUUID,
	}, "UUID").First(&uploadSession); res.Error != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Upload Session not found")
	}

	if uploadSession.UserID != userId {
		return c.Status(fiber.StatusBadRequest).SendString("Upload Session not found")
	}

	if res := inits.DB.
		Model(&models.UploadChunck{}).
		Where(&models.UploadChunck{
			UploadSessionID: uploadSession.ID,
		}).
		Delete(&models.UploadChunck{}); res.Error != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove upload chuncks from database (%d): %v\n", uploadSession.ID, res.Error)
	}
	if res := inits.DB.
		Delete(&models.UploadSession{}, uploadSession.ID); res.Error != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove upload session from database (%d): %v\n", uploadSession.ID, res.Error)
	}

	if err := os.RemoveAll(uploadSession.SessionFolder); err != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove session folder: %v\n", err)
	}

	return c.Status(fiber.StatusOK).SendString("ok")
}
