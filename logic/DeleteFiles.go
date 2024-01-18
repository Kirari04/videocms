package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func DeleteFiles(fileValidation *models.LinksDeleteValidation, userID uint) (status int, err error) {
	if len(fileValidation.LinkIDs) == 0 {
		return http.StatusBadRequest, errors.New("array LinkIDs is empty")
	}
	if int64(len(fileValidation.LinkIDs)) > config.ENV.MaxItemsMultiDelete {
		return http.StatusBadRequest, errors.New("max requested items exceeded")
	}

	//check if requested files exists
	linkIdDeleteMap := make(map[uint]bool, len(fileValidation.LinkIDs))
	linkIdDeleteList := []uint{}
	for _, LinkValidation := range fileValidation.LinkIDs {
		if res := inits.DB.First(&models.Link{
			UserID: userID,
		}, LinkValidation.LinkID); res.Error != nil {
			return http.StatusBadRequest, fmt.Errorf("linkID (%d) doesn't exist", LinkValidation.LinkID)
		}
		if linkIdDeleteMap[LinkValidation.LinkID] {
			return http.StatusBadRequest, fmt.Errorf("the files have to be distinct. File %d is dublicate", LinkValidation.LinkID)
		}
		linkIdDeleteList = append(linkIdDeleteList, LinkValidation.LinkID)
		linkIdDeleteMap[LinkValidation.LinkID] = true
	}

	// delete links
	if res := inits.DB.Delete(&models.Link{}, linkIdDeleteList); res.Error != nil {
		log.Printf("Failed to delete links: %v", res.Error)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	return http.StatusOK, nil
}
