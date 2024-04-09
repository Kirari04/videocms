package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func DeleteTag(tagId uint, fromLinkId uint, userId uint) (status int, err error) {
	//check if requested folder exists (if set)
	var link models.Link
	if err := inits.DB.First(&link, fromLinkId).Error; err != nil {
		return http.StatusBadRequest, errors.New("link doesn't exist")
	}
	if link.UserID != userId {
		return http.StatusBadRequest, errors.New("link doesn't exist")
	}

	// check if tag exists
	var tag models.Tag
	if err := inits.DB.First(&tag, tagId).Error; err != nil {
		return http.StatusBadRequest, errors.New("tag doesn't exist")
	}

	if tag.UserId != userId {
		return http.StatusBadRequest, errors.New("tag doesn't exist")
	}

	if err := inits.DB.Model(&link).Association("Tags").Delete(&tag); err != nil {
		log.Printf("Error removing tag: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	return http.StatusOK, nil
}
