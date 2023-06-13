package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

func GetSubtitleData(fileName string, UUID string, SUBUUID string) (status int, filePath *string, err error) {
	reFILE := regexp.MustCompile(`^out\.(ass)$`)

	if !reFILE.MatchString(fileName) {
		return fiber.StatusBadRequest, nil, errors.New("bad file format")
	}

	//translate link id to file id
	var dbLink models.Link

	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Subtitles").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return fiber.StatusNotFound, nil, errors.New("subtitle doesn't exist")
	}

	//check if subtitle uuid exists
	subExists := false
	for _, sub := range dbLink.File.Subtitles {
		if sub.Ready &&
			sub.UUID == SUBUUID {
			subExists = true
		}
	}
	if !subExists {
		return fiber.StatusNotFound, nil, errors.New("subtitle doesn't exist")
	}

	fileRes := fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPriv, dbLink.File.UUID, SUBUUID, fileName)

	return fiber.StatusOK, &fileRes, nil
}
