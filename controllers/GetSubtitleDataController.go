package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

func GetSubtitleData(c *fiber.Ctx) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122"`
		SUBUUID string `validate:"required,uuid_rfc4122"`
		FILE    string `validate:"required"`
	}
	var requestValidation Request
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	reFILE := regexp.MustCompile(`^out\.(ass)$`)

	if !reFILE.MatchString(requestValidation.FILE) {
		return c.Status(400).SendString("Bad file format")
	}

	//translate link id to file id
	var dbLink models.Link

	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Subtitles").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return c.Status(fiber.StatusNotFound).SendString("Subtitle doesn't exist")
	}

	//check if subtitle uuid exists
	subExists := false
	for _, sub := range dbLink.File.Subtitles {
		if sub.Ready &&
			sub.UUID == requestValidation.SUBUUID {
			subExists = true
		}
	}
	if !subExists {
		return c.Status(fiber.StatusNotFound).SendString("Subtitle doesn't exist")
	}

	filePath := fmt.Sprintf("./videos/qualitys/%s/%s/%s", dbLink.File.UUID, requestValidation.SUBUUID, requestValidation.FILE)

	if err := c.SendFile(filePath); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Subtitle doesn't exist")
	}
	return nil
}
