package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

type UpdateUserRequest struct {
	Username string   `json:"username"`
	Email    string   `json:"email" validate:"omitempty,email"`
	Admin    *bool    `json:"admin"`
	Storage  *int64   `json:"storage"`
	Balance  *float64 `json:"balance"`
}

func UpdateUser(c echo.Context) error {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid User ID")
	}

	req := new(UpdateUserRequest)
	if status, err := helpers.Validate(c, req); err != nil {
		return c.String(status, err.Error())
	}

	status, user, err := logic.UpdateUser(id, req.Username, req.Email, req.Admin, req.Storage, req.Balance)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, user)
}