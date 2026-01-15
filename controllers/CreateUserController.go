package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"net/http"

	"github.com/labstack/echo/v4"
)

type CreateUserRequest struct {
	Username string  `json:"username" validate:"required,min=3,max=32"`
	Password string  `json:"password" validate:"required,min=8,max=250"`
	Email    string  `json:"email" validate:"required,email"`
	Admin    bool    `json:"admin"`
	Storage  int64   `json:"storage"`
	Balance  float64 `json:"balance"`
}

func CreateUser(c echo.Context) error {
	req := new(CreateUserRequest)
	if status, err := helpers.Validate(c, req); err != nil {
		return c.String(status, err.Error())
	}

	status, user, err := logic.CreateUser(req.Username, req.Password, req.Email, req.Admin, req.Storage, req.Balance)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(http.StatusCreated, user)
}