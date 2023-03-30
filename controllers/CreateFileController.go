package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
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
		return c.Status(400).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(400).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("No File uploaded")
	}

	fileId := uuid.NewString()
	fileSplit := strings.Split(file.Filename, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	filePath := fmt.Sprintf("./videos/%s.%s", fileId, fileExt)

	// Save file to storage
	if err := c.SaveFile(file, filePath); err != nil {
		log.Printf("Failed to save file: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	//check if requested folder exists (if set)
	if fileValidation.ParentFolderID > 0 {
		res := inits.DB.First(&models.Folder{}, fileValidation.ParentFolderID)
		if res.Error != nil {
			return c.Status(400).SendString("Parent folder doesn't exist")
		}
	}

	// business logic
	status, dbLink, err := logic.CreateFile(filePath, fileValidation.ParentFolderID, file.Filename, fileId, file.Size, c.Locals("UserID").(uint))
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(dbLink)
}
