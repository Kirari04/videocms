package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func UpdateFile(c echo.Context) error {
	// parse & validate request
	var linkValidation models.LinkUpdateValidation
	if status, err := helpers.Validate(c, &linkValidation); err != nil {
		return c.String(status, err.Error())
	}

	var dbLink models.Link
	//check if requested file /link id exists
	if res := inits.DB.First(&dbLink, linkValidation.LinkID); res.Error != nil {
		return c.String(http.StatusBadRequest, "File doesn't exist")
	}

	if linkValidation.ParentFolderID > 0 {
		if res := inits.DB.First(&models.Folder{}, linkValidation.ParentFolderID); res.Error != nil {
			return c.String(http.StatusBadRequest, "Parent folder doesn't exist")
		}
	}

	//update link data
	dbLink.Name = linkValidation.Name
	dbLink.ParentFolderID = linkValidation.ParentFolderID
	if res := inits.DB.Save(&dbLink); res.Error != nil {
		log.Printf("Failed to update link: %v", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}
