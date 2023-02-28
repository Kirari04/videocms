package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func GetFile(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.FileGetValidation
	if err := c.QueryParser(&fileValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}

	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	userID := c.Locals("UserID").(uint)

	// query all files
	var link models.Link
	res := inits.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Subtitles").
		Where(&models.Link{
			UserID: userID,
		}).
		Find(&link, fileValidation.FileID)
	if res.Error != nil {
		log.Fatalf("Failed to query file: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	type Response struct {
		Link      models.Link
		File      models.File
		Qualitys  []models.Quality
		Subtitles []models.Subtitle
	}
	response := Response{
		Link:      link,
		File:      link.File,
		Qualitys:  link.File.Qualitys,
		Subtitles: link.File.Subtitles,
	}
	// return value
	return c.JSON(response)
}
