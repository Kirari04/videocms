package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func DeleteTagController(c echo.Context) error {
	// parse & validate request
	var validator models.TagDeleteValidation
	if status, err := helpers.Validate(c, &validator); err != nil {
		return c.String(status, err.Error())
	}

	status, err := logic.DeleteTag(validator.TagID, validator.FileID, c.Get("UserID").(uint))

	if err != nil {
		return c.String(status, err.Error())
	}

	return c.NoContent(status)
}
