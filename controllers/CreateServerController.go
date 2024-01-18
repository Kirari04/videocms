package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/thanhpk/randstr"
)

func CreateServer(c echo.Context) error {
	// parse & validate request
	var validatus models.ServerCreateValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	var existing int64
	if res := inits.DB.Model(&models.Server{}).Where(&models.Server{
		Hostname: validatus.Hostname,
	}).Count(&existing); res.Error != nil {
		c.Logger().Error("Failed to count server", res.Error)
		return c.NoContent(fiber.StatusInternalServerError)
	}
	if existing > 0 {
		return c.String(http.StatusBadRequest, "Hostname already used")
	}

	server := models.Server{
		UUID:     uuid.NewString(),
		Hostname: validatus.Hostname,
		Token:    randstr.Base62(512),
	}
	if res := inits.DB.Create(&server); res.Error != nil {
		c.Logger().Error("Failed to create server", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"ID":       server.ID,
		"UUID":     server.UUID,
		"Hostname": server.Hostname,
		"Token":    server.Token,
	})
}
