package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/labstack/echo/v4"
)

func DeleteServer(c echo.Context) error {
	// parse & validate request
	var validatus models.ServerDeleteValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	res := inits.DB.Delete(&models.Server{}, validatus.ServerID)
	if res.Error != nil {
		c.Logger().Error("Failed to delete server", validatus.ServerID, res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	if res.RowsAffected == 0 {
		return c.String(http.StatusBadRequest, "Server not found")
	}
	return c.String(http.StatusOK, "ok")
}
