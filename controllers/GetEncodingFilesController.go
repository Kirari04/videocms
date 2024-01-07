package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

type GetEncodingFilesRes struct {
	ID       uint
	Name     string
	Quality  string
	Progress float64
}

func GetEncodingFiles(c *fiber.Ctx) error {
	userId, ok := c.Locals("UserID").(uint)
	if !ok {
		log.Println("Failed to catch user")
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	var res []GetEncodingFilesRes
	if err := inits.DB.
		Model(&models.Link{}).
		Select(
			"links.id as id",
			"links.name as name",
			"qualities.name as quality",
			"qualities.progress as progress",
		).
		Where("links.user_id = ?", userId).
		Joins("JOIN files ON files.id = links.file_id").
		Joins("JOIN qualities ON files.id = qualities.file_id AND qualities.encoding = ? AND qualities.failed = ? AND qualities.ready = ?", true, false, false).
		Scan(&res).Error; err != nil {
		log.Println("Failed to list encoding files", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(&res)
}
