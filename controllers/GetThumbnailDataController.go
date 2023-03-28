package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

func GetThumbnailData(c *fiber.Ctx) error {
	type Request struct {
		UUID string `validate:"required,uuid_rfc4122"`
		FILE string `validate:"required"`
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

	reFILE := regexp.MustCompile(`^[1-4]x[1-4]\.(webp)$`)

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
		return c.Status(fiber.StatusNotFound).SendString("Thumbnail doesn't exist")
	}

	filePath := fmt.Sprintf("./videos/qualitys/%s/%s", dbLink.File.UUID, requestValidation.FILE)

	return c.SendFile(filePath)
}
