package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/thanhpk/randstr"
)

func CreateServer(c *fiber.Ctx) error {
	// parse & validate request
	var validatus models.ServerCreateValidation
	if err := c.BodyParser(&validatus); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validatus); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}
	var existing int64
	if res := inits.DB.Model(&models.Server{}).Where(&models.Server{
		Hostname: validatus.Hostname,
	}).Count(&existing); res.Error != nil {
		log.Println("Failed to count server", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if existing > 0 {
		return c.Status(fiber.StatusBadRequest).SendString("Hostname already used")
	}

	server := models.Server{
		UUID:     uuid.NewString(),
		Hostname: validatus.Hostname,
		Token:    randstr.Base62(512),
	}
	if res := inits.DB.Create(&server); res.Error != nil {
		log.Println("Failed to create server", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"ID":       server.ID,
		"UUID":     server.UUID,
		"Hostname": server.Hostname,
		"Token":    server.Token,
	})
}
