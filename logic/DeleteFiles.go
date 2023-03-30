package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func DeleteFiles(fileValidation *models.LinksDeleteValidation, userID uint) (status int, err error) {
	if helpers.UserRequestAsyncObj.Blocked(userID) {
		return fiber.StatusTooManyRequests, errors.New("Wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userID)
	defer helpers.UserRequestAsyncObj.End(userID)

	if len(fileValidation.LinkIDs) == 0 {
		return fiber.StatusBadRequest, errors.New("array LinkIDs is empty")
	}
	if int64(len(fileValidation.LinkIDs)) > config.ENV.MaxItemsMultiDelete {
		return fiber.StatusBadRequest, errors.New("max requested items exceeded")
	}

	//check if requested files exists
	linkIdDeleteMap := make(map[uint]bool, len(fileValidation.LinkIDs))
	linkIdDeleteList := []uint{}
	for _, LinkValidation := range fileValidation.LinkIDs {
		if res := inits.DB.First(&models.Link{
			UserID: userID,
		}, LinkValidation.LinkID); res.Error != nil {
			return fiber.StatusBadRequest, fmt.Errorf("LinkID (%d) doesn't exist", LinkValidation.LinkID)
		}
		if linkIdDeleteMap[LinkValidation.LinkID] {
			return fiber.StatusBadRequest, fmt.Errorf("The files have to be distinct. File %d is dublicate", LinkValidation.LinkID)
		}
		linkIdDeleteList = append(linkIdDeleteList, LinkValidation.LinkID)
		linkIdDeleteMap[LinkValidation.LinkID] = true
	}

	// delete links
	if res := inits.DB.Delete(&models.Link{}, linkIdDeleteList); res.Error != nil {
		log.Printf("Failed to delete links: %v", res.Error)
		return fiber.StatusInternalServerError, errors.New("")
	}

	for _, linkId := range linkIdDeleteList {
		// check if any links left, else (=0) delete original file too
		var countLinks int64
		if res := inits.DB.
			Model(&models.Link{}).
			Where(&models.Link{
				FileID: linkId,
			}).
			Count(&countLinks); res.Error != nil {
			log.Printf("Failed to delete link: %v", res.Error)
			return fiber.StatusInternalServerError, errors.New("")
		}

		if countLinks == 0 {
			// delete file
			if res := inits.DB.Delete(&models.File{}, linkId); res.Error != nil {
				log.Printf("Failed to delete file: %v", res.Error)
				return fiber.StatusInternalServerError, errors.New("")
			}
		}
	}

	return fiber.StatusOK, nil
}
