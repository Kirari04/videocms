package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// this route is not securet with user jwt token so it doesnt invalidate the chunck because the session invalidated during the upload time
func CreateUploadChunck(c *fiber.Ctx) error {
	// parse & validate request
	var validation models.UploadChunckValidation
	if err := c.BodyParser(&validation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	// file validation
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("No File uploaded")
	}

	fileId := uuid.NewString()
	fileSplit := strings.Split(file.Filename, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	filePath := fmt.Sprintf("%s/%s.%s", config.ENV.FolderVideoUploadsPriv, fileId, fileExt)

	// Save file to storage
	if err := c.SaveFile(file, filePath); err != nil {
		log.Printf("Failed to save file: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// business logic
	status, response, err := logic.CreateUploadChunck(*validation.Index, validation.SessionJwtToken, filePath)
	if err != nil {
		os.Remove(filePath)
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(response)
}
