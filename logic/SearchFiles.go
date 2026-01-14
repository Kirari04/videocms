package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func SearchFiles(userId uint, query string) (status int, response *[]models.Link, err error) {
	// query all files matching the search query
	var links []models.Link
	res := inits.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Preload("Tags").
		Where("user_id = ? AND name LIKE ?", userId, "%"+query+"%").
		Order("name ASC").
		Find(&links)
	if res.Error != nil {
		log.Printf("Failed to search files: %v", res.Error)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	return http.StatusOK, &links, nil
}
