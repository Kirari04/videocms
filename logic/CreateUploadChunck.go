package logic

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"math"
	"os"

	"github.com/gofiber/fiber/v2"
)

func CreateUploadChunck(index uint, sessionToken string, fromFile string, userId uint) (status int, response string, err error) {
	// validate token
	token, claims, err := helpers.VerifyDynamicJWT(sessionToken, &models.UploadSessionClaims{})
	if err != nil && claims != nil {
		log.Printf("err: %v", err)
		return fiber.StatusBadRequest, "", errors.New("broken upload session token")
	}
	if !token.Valid {
		return fiber.StatusBadRequest, "", errors.New("invalid upload session token")
	}
	if (*claims).UserID != userId {
		return fiber.StatusForbidden, "", fiber.ErrForbidden
	}

	//check if session still active
	uploadSession := models.UploadSession{}
	if res := inits.DB.
		Where(&models.UploadSession{
			UUID: (*claims).UUID,
		}).First(&uploadSession); res.Error != nil {
		return fiber.StatusNotFound, "", errors.New("upload session not found")
	}

	// check chunck size
	chunckFile, err := os.Open(fromFile)
	if err != nil {
		log.Printf("Failed to open uploaded chunck: %v", err)
		return fiber.StatusInternalServerError, "", fiber.ErrInternalServerError
	}
	chunckFileStat, err := chunckFile.Stat()
	if err != nil {
		log.Printf("Failed to read stat from uploaded chunck: %v", err)
		return fiber.StatusInternalServerError, "", fiber.ErrInternalServerError
	}
	maxchunckFileSize := int64(math.Ceil(float64(uploadSession.Size) / float64(uploadSession.ChunckCount)))
	if chunckFileStat.Size() > maxchunckFileSize+100 {
		return fiber.StatusRequestEntityTooLarge, "", fiber.ErrRequestEntityTooLarge
	}

	// check chunck count
	if int(index) >= uploadSession.ChunckCount {
		return fiber.StatusBadRequest, "", fmt.Errorf("chunck index is too high: chunck index: %d vs max index: %d", index, uploadSession.ChunckCount)
	}

	/*
		Because of parallelism we don't check if the index has already been uploaded.
		Incase it already has been uploaded the new one will just overwrite the old one.
	*/
	chunckPath := fmt.Sprintf("%s/%v.chunck", uploadSession.SessionFolder, index)
	if err := os.Rename(fromFile, chunckPath); err != nil {
		log.Printf("Failed to move uploaded chunck into upload session folder: %v", err)
		return fiber.StatusInternalServerError, "", fiber.ErrInternalServerError
	}

	existingUploadedChunck := models.UploadChunck{}
	if res := inits.DB.Where(&models.UploadChunck{
		Index:           index,
		Path:            chunckPath,
		UploadSessionID: uploadSession.ID,
	}).FirstOrCreate(&existingUploadedChunck); res.Error != nil {
		log.Printf("Failed to add uploaded chunck into db: %v", res.Error)
		log.Printf("Removing Chunck: %v", os.Remove(chunckPath))
		return fiber.StatusInternalServerError, "", fiber.ErrInternalServerError
	}

	return fiber.StatusOK, "ok", nil
}
