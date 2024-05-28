package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetUsers(c echo.Context) error {
	users := make([]models.User, 0)
	if res := inits.DB.First(&users); res.Error != nil {
		log.Println("Failed to fetch users")
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &users)
}
