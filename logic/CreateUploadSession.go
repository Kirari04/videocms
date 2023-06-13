package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

type CreateUploadSessionResponse struct {
	Token   string
	UUID    string
	Expires time.Time
}

/*
This functions shouldn't be run concurrent
else the user would be able to spam the endpoint and
use the split delay in the db lookup to have more concurrent upload sessions then defined
*/
func CreateUploadSession(fileHash string, toFolder uint, fileName string, uploadSessionUUID string, fileSize int64, userId uint) (status int, response *CreateUploadSessionResponse, err error) {

	if helpers.UserRequestAsyncObj.Blocked(userId) {
		return fiber.StatusTooManyRequests, nil, errors.New("wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userId)
	defer helpers.UserRequestAsyncObj.End(userId)

	//check if requested folder exists (if set)
	if toFolder > 0 {
		res := inits.DB.First(&models.Folder{}, toFolder)
		if res.Error != nil {
			return fiber.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	//check requested filesize size
	if fileSize > config.ENV.MaxUploadFilesize {
		return fiber.StatusBadRequest, nil, fmt.Errorf("exceeded max upload filesize: %v", config.ENV.MaxUploadFilesize)
	}

	//check for active upload sessions
	var activeUploadSessions int64
	if res := inits.DB.Where(&models.UploadSession{
		UserID: userId,
	}).Count(&activeUploadSessions); res.Error != nil {
		log.Printf("Failed to calc activeUploadSessions: %v : %v", activeUploadSessions, res.Error)
		return fiber.StatusInternalServerError, nil, fiber.ErrInternalServerError
	}
	if activeUploadSessions >= config.ENV.MaxUploadSessions {
		return fiber.StatusBadRequest, nil, fmt.Errorf("exceeded max upload sessions: %v", config.ENV.MaxUploadSessions)
	}

	// create session
	newSession := models.UploadSession{
		Name:           fileName,
		UUID:           uploadSessionUUID,
		Hash:           fileHash,
		Size:           fileSize,
		SessionFolder:  fmt.Sprintf("%s/%s", config.ENV.FolderVideoUploadsPriv, uploadSessionUUID),
		ParentFolderID: toFolder,
		UserID:         userId,
	}
	if res := inits.DB.Create(&newSession); res.Error != nil {
		log.Printf("Failed to create new upload session: %v", res.Error)
		return fiber.StatusInternalServerError, nil, fiber.ErrInternalServerError
	}

	claims := models.UploadSessionClaims{
		UUID:   uploadSessionUUID,
		UserID: userId,
	}

	maxUploadDuration := time.Hour * 2
	token, expirationTime, err := helpers.GenerateDynamicJWT[models.UploadSessionClaims](&claims, maxUploadDuration)
	if err != nil {
		log.Printf("Failed to generate jwt token for upload session: %v", err)
		return fiber.StatusInternalServerError, nil, fiber.ErrInternalServerError
	}

	return fiber.StatusOK, &CreateUploadSessionResponse{
		Token:   token,
		Expires: expirationTime,
		UUID:    uploadSessionUUID,
	}, nil
}
