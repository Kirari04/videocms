package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func CreateTag(tagName string, toLinkId uint, userId uint) (status int, newTag *models.Tag, err error) {
	//check if requested folder exists (if set)
	var link models.Link
	if err := inits.DB.First(&link, toLinkId).Error; err != nil {
		return http.StatusBadRequest, nil, errors.New("link doesn't exist")
	}
	if link.UserID != userId {
		return http.StatusBadRequest, nil, errors.New("link doesn't exist")
	}

	// check if tag already exists else create new
	var tag models.Tag
	if err := inits.DB.Where(&models.Tag{Name: tagName, UserId: userId}).First(&tag).Error; err != nil {
		tag = models.Tag{Name: tagName, UserId: userId}
		if err := inits.DB.Create(&tag).Error; err != nil {
			return http.StatusBadRequest, nil, errors.New("failed to create new tag")
		}
	}

	if err := inits.DB.Model(&link).Association("Tags").Append(&tag); err != nil {
		log.Printf("Error adding new tag: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	return http.StatusOK, &tag, nil
}
