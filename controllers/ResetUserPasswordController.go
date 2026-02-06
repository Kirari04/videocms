package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

type ResetPasswordRequest struct {
	NewPassword string `json:"new_password" validate:"required,min=8,max=250"`
}

func ResetUserPassword(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid User ID")
	}

	req := new(ResetPasswordRequest)
	if status, err := helpers.Validate(c, req); err != nil {
		return c.String(status, err.Error())
	}

	status, err := logic.ResetUserPassword(id, req.NewPassword)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, map[string]string{"message": "Password updated successfully"})
}
