package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

/*
 */
func CreateUploadFile(sessionToken string, userId uint) (status int, response *models.Link, err error) {
	// validate token
	token, claims, err := helpers.VerifyDynamicJWT(sessionToken, &models.UploadSessionClaims{}, []byte(config.ENV.JwtUploadSecretKey))
	if err != nil && claims != nil {
		log.Printf("err: %v", err)
		return http.StatusBadRequest, nil, errors.New("broken upload session token")
	}
	if !token.Valid {
		return http.StatusBadRequest, nil, errors.New("invalid upload session token")
	}
	if (*claims).UserID != userId {
		return http.StatusForbidden, nil, echo.ErrForbidden
	}

	//check if session still active
	uploadSession := models.UploadSession{}
	if res := inits.DB.
		Where(&models.UploadSession{
			UUID:   (*claims).UUID,
			UserID: userId,
		}).First(&uploadSession); res.Error != nil {
		return http.StatusNotFound, nil, errors.New("upload session not found")
	}

	//list all chuncks
	uploadChuncks := []models.UploadChunck{}
	if res := inits.DB.
		Where(&models.UploadChunck{
			UploadSessionID: uploadSession.ID,
		}).
		Order("`index` ASC").
		Find(&uploadChuncks); res.Error != nil {
		log.Printf("Failed to create find upload chuncks: %v", res.Error)
		return http.StatusNotFound, nil, errors.New("upload chuncks not found")
	}
	if len(uploadChuncks) != uploadSession.ChunckCount {
		return http.StatusBadRequest, nil, fmt.Errorf("missing Chuncks: uploaded %v, required %v", len(uploadChuncks), uploadSession.ChunckCount)
	}

	// delete any missing files or sessions inside database if anything failes or it successfully finishes
	defer createUploadFileCleanup(&uploadSession)

	// open finalFile (copy destination of the chuncks)
	finalFilePath := fmt.Sprintf("%v/example.mkv", uploadSession.SessionFolder)
	finalFile, err := os.OpenFile(finalFilePath, os.O_CREATE|os.O_WRONLY, 0766)
	if err != nil {
		log.Printf("Failed to create final file: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	// copy uploaded chuncks into final file
	start := time.Now()
	var written int64
	for _, uploadChunck := range uploadChuncks {
		openedChunck, err := os.Open(uploadChunck.Path)
		if err != nil {
			log.Printf("Failed to read chunck %v: %v", uploadChunck.Path, err)
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
		n, err := io.Copy(finalFile, openedChunck)
		if err != nil {
			log.Printf("Failed to copy chunck %v: %v", uploadChunck.Path, err)
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
		written += n
		if err := openedChunck.Close(); err != nil {
			log.Printf("Failed to close chunck %v: %v", uploadChunck.Path, err)
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
	}

	if err := finalFile.Close(); err != nil {
		log.Printf("Failed to close final file: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	reconstructionDuration := time.Since(start).Seconds()

	// check file size
	finalFilePathInfo, err := os.Stat(finalFilePath)
	if err != nil {
		log.Printf("Failed to read filestat of final file: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	if finalFilePathInfo.Size() != uploadSession.Size {
		return http.StatusConflict, nil, fmt.Errorf("the uploaded file size doesnt match with the uploaded file: server %v, client %v", finalFilePathInfo.Size(), uploadSession.Size)
	}

	// create file
	fileId := uuid.NewString()
	filePath := fmt.Sprintf("%s/%s.%s", config.ENV.FolderVideoUploadsPriv, fileId, "tmp")
	if err := os.Rename(finalFilePath, filePath); err != nil {
		log.Printf("Failed to copy final file to destination: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	status, dbLink, cloned, err := CreateFile(&filePath, uploadSession.ParentFolderID, uploadSession.Name, fileId, uploadSession.Size, userId, uploadSession.UUID)
	if err != nil {
		os.Remove(filePath)
		return status, nil, err
	}
	if cloned {
		os.Remove(filePath)
	}

	// Update UploadLog with the newly created FileID
	if res := inits.DB.Model(&models.UploadLog{}).
		Where("upload_session_id = ?", uploadSession.ID).
		Update("file_id", dbLink.FileID); res.Error != nil {
		log.Printf("[WARNING] Failed to update UploadLog with FileID: %v\n", res.Error)
	}

	helpers.TrackEncoding(userId, dbLink.FileID, "reconstruction", reconstructionDuration)

	return status, dbLink, nil
}

func createUploadFileCleanup(uploadSession *models.UploadSession) {
	if err := os.RemoveAll(uploadSession.SessionFolder); err != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove session folder: %v\n", err)
	}
	if res := inits.DB.
		Model(&models.UploadChunck{}).
		Where(&models.UploadChunck{
			UploadSessionID: uploadSession.ID,
		}).
		Delete(&models.UploadChunck{}); res.Error != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove upload chuncks from database (%d): %v\n", uploadSession.ID, res.Error)
	}
	if res := inits.DB.
		Delete(&models.UploadSession{}, uploadSession.ID); res.Error != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove upload session from database (%d): %v\n", uploadSession.ID, res.Error)
	}
}
