package controllers

import (
	"ch/kirari04/videocms/logic"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

func GetUser(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid User ID")
	}

	status, user, err := logic.GetUser(id)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, user)
}