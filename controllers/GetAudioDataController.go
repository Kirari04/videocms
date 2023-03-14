package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

func GetAudioData(c *fiber.Ctx) error {
	type Request struct {
		UUID      string `validate:"required,uuid_rfc4122"`
		AUDIOUUID string `validate:"required,uuid_rfc4122"`
		FILE      string `validate:"required"`
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

	reFILE := regexp.MustCompile(`^audio[0-9]{0,4}\.(m3u8|ts|wav|mp3|ogg)$`)

	if !reFILE.MatchString(requestValidation.FILE) {
		return c.Status(400).SendString("Bad file format")
	}

	//translate link id to file id
	var dbLink models.Link

	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return c.Status(fiber.StatusNotFound).SendString("Audio doesn't exist")
	}

	//check if audio uuid exists
	audioExists := false
	for _, audio := range dbLink.File.Audios {
		if audio.Ready &&
			audio.UUID == requestValidation.AUDIOUUID {
			audioExists = true
		}
	}
	if !audioExists {
		return c.Status(fiber.StatusNotFound).SendString("Audio doesn't exist")
	}

	filePath := fmt.Sprintf("./videos/qualitys/%s/%s/%s", dbLink.File.UUID, requestValidation.AUDIOUUID, requestValidation.FILE)

	if err := c.SendFile(filePath); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Audio doesn't exist")
	}
	return nil
}
