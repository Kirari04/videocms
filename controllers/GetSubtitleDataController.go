package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"os"
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

	reFILE := regexp.MustCompile(`^out\.(vtt)$`)

	if !reFILE.MatchString(requestValidation.FILE) {
		return c.Status(400).SendString("Bad file format")
	}

	//translate link id to file id
	var dbLink models.Link

	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return c.Status(fiber.StatusNotFound).SendString("Link doesn't exist")
	}

	filePath := fmt.Sprintf("./videos/qualitys/%s/%s/%s", dbLink.File.UUID, requestValidation.SUBUUID, requestValidation.FILE)

	file, err := os.Open(filePath)
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("File not found - lol")
	}
	return c.SendStream(file)
}
