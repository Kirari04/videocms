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

func CreateFile(c *fiber.Ctx) error {
	if !*config.ENV.UploadEnabled {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Upload has been desabled")
	}

	// parse & validate request
	var fileValidation models.FileCreateValidation
	if err := c.BodyParser(&fileValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

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
	status, dbLink, cloned, err := logic.CreateFile(filePath, fileValidation.ParentFolderID, file.Filename, fileId, file.Size, c.Locals("UserID").(uint))
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}
	if err != nil || cloned {
		os.Remove(filePath)
	}

	return c.Status(status).JSON(dbLink)
}
