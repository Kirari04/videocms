package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/labstack/echo/v4"
)

type CreateUploadSessionResponse struct {
	Token       string
	UUID        string
	ChunckCount int
	Expires     time.Time
}

/*
This functions shouldn't be run concurrent
else the user would be able to spam the endpoint and
use the split delay in the db lookup to have more concurrent upload sessions then defined
*/
func CreateUploadSession(toFolder uint, fileName string, uploadSessionUUID string, fileSize int64, userId uint) (status int, response *CreateUploadSessionResponse, err error) {

	if helpers.UserRequestAsyncObj.Blocked(userId) {
		return http.StatusTooManyRequests, nil, errors.New("wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userId)
	defer helpers.UserRequestAsyncObj.End(userId)

	//check if requested folder exists (if set)
	if toFolder > 0 {
		res := inits.DB.First(&models.Folder{}, toFolder)
		if res.Error != nil {
			return http.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	//check requested filesize size
	if fileSize > config.ENV.MaxUploadFilesize {
		return http.StatusRequestEntityTooLarge, nil, fmt.Errorf("exceeded max upload filesize: %v", config.ENV.MaxUploadFilesize)
	}

	// get user settings
	User, err := helpers.GetUser(userId)
	if err != nil {
		log.Printf("Failed to fetch user %v: %v", userId, err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	//check for active upload sessions
	var activeUploadSessions int64
	if res := inits.DB.
		Model(&models.UploadSession{}).
		Where(&models.UploadSession{
			UserID: userId,
		}).Count(&activeUploadSessions); res.Error != nil {
		log.Printf("Failed to calc activeUploadSessions: %v : %v", activeUploadSessions, res.Error)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	if activeUploadSessions >= config.ENV.MaxUploadSessions {
		if activeUploadSessions >= User.Settings.UploadSessionsMax {
			return http.StatusBadRequest, nil, fmt.Errorf("exceeded max upload sessions: %v", config.ENV.MaxUploadSessions)
		}
	}

	// create upload session folder
	sessionFolder := fmt.Sprintf("%s/%s", config.ENV.FolderVideoUploadsPriv, uploadSessionUUID)
	if err := os.MkdirAll(sessionFolder, 0766); err != nil {
		log.Printf("Failed to create upload session folder: %v : %v", sessionFolder, err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	// create session
	chunckCount := math.Ceil(float64(fileSize) / float64(config.ENV.MaxUploadChuncksize))
	newSession := models.UploadSession{
		Name:           fileName,
		UUID:           uploadSessionUUID,
		Size:           fileSize,
		ChunckCount:    int(chunckCount),
		SessionFolder:  sessionFolder,
		ParentFolderID: toFolder,
		UserID:         userId,
	}
	if res := inits.DB.Create(&newSession); res.Error != nil {
		log.Printf("Failed to create new upload session: %v", res.Error)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	claims := models.UploadSessionClaims{
		UUID:   uploadSessionUUID,
		UserID: userId,
	}

	maxUploadDuration := time.Hour * 2
	token, expirationTime, err := helpers.GenerateDynamicJWT(&claims, maxUploadDuration, []byte(config.ENV.JwtUploadSecretKey))
	if err != nil {
		log.Printf("Failed to generate jwt token for upload session: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	return http.StatusOK, &CreateUploadSessionResponse{
		Token:       token,
		Expires:     expirationTime,
		UUID:        uploadSessionUUID,
		ChunckCount: int(chunckCount),
	}, nil
}
