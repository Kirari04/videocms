package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

func GetThumbnailData(fileName string, UUID string) (status int, filePath *string, err error) {
	reFILE := regexp.MustCompile(`^[1-4]x[1-4]\.(webp)$`)

	if !reFILE.MatchString(fileName) {
		return fiber.StatusBadRequest, nil, errors.New("bad file format")
	}

	//translate link id to file id
	var dbLink models.Link
	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return fiber.StatusNotFound, nil, errors.New("thumbnail doesn't exist")
	}

	fileRes := fmt.Sprintf("./videos/qualitys/%s/%s", dbLink.File.UUID, fileName)

	return fiber.StatusOK, &fileRes, nil
}
