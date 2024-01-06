package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

type GetUploadSessionsRes struct {
	CreatedAt   *time.Time
	Name        string
	UUID        string
	Size        int64
	ChunckCount int
}

func GetUploadSessions(c *fiber.Ctx) error {
	userId, ok := c.Locals("UserID").(uint)
	if !ok {
		log.Println("GetUploadSessions: Failed to catch userId")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	var sessions []GetUploadSessionsRes
	if res := inits.DB.
		Model(&models.UploadSession{}).
		Where(&models.UploadSession{
			UserID: userId,
		}, "UserID").
		Find(&sessions); res.Error != nil {
		log.Println("Failed to list upload sessions", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(&sessions)
}
