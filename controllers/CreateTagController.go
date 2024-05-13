package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func CreateTagController(c echo.Context) error {
	// parse & validate request
	var validator models.TagCreateValidation
	if status, err := helpers.Validate(c, &validator); err != nil {
		return c.String(status, err.Error())
	}

	status, dbTag, err := logic.CreateTag(validator.Name, validator.LinkId, c.Get("UserID").(uint))

	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, dbTag)
}
