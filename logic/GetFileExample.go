package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetFileExample() (status int, response string, err error) {
	var link models.Link
	if res := inits.DB.First(&link); res.Error != nil {
		return http.StatusNotFound, "", echo.ErrNotFound
	}
	return http.StatusOK, link.UUID, nil
}
