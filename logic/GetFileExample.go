package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"

	"github.com/gofiber/fiber/v2"
)

func GetFileExample() (status int, response string, err error) {
	var link models.Link
	if res := inits.DB.First(&link); res.Error != nil {
		return fiber.StatusNotFound, "", errors.New(fiber.ErrNotFound.Message)
	}
	return fiber.StatusOK, link.UUID, nil
}
