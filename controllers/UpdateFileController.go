package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) UpdateFile(c echo.Context) error {
	// parse & validate request
	var linkValidation models.LinkUpdateValidation
	if status, err := helpers.Validate(c, &linkValidation); err != nil {
		return c.String(status, err.Error())
	}

	var dbLink models.Link
	query := h.Deps.DB
	if isAdmin, _ := c.Get("Admin").(bool); !isAdmin {
		query = query.Where("user_id = ?", c.Get("UserID").(uint))
	}
	//check if requested file /link id exists
	if res := query.First(&dbLink, linkValidation.LinkID); res.Error != nil {
		return c.String(http.StatusBadRequest, "File doesn't exist")
	}

	//update link data
	dbLink.Name = linkValidation.Name
	if res := h.Deps.DB.Save(&dbLink); res.Error != nil {
		log.Printf("Failed to update link: %v", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}
