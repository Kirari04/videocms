package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// this route is not securet with user jwt token so it doesnt invalidate the chunck because the session invalidated during the upload time
func CreateUploadChunck(c echo.Context) error {
	// parse & validate request
	var validation models.UploadChunckValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	// file validation
	file, err := c.FormFile("file")
	if err != nil {
		return c.String(http.StatusBadRequest, "No File uploaded")
	}
	src, err := file.Open()
	if err != nil {
		c.Logger().Error("Failed to open src file", err)
		return c.NoContent(fiber.StatusInternalServerError)
	}
	defer src.Close()

	fileId := uuid.NewString()
	fileSplit := strings.Split(file.Filename, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	filePath := fmt.Sprintf("%s/%s.%s", config.ENV.FolderVideoUploadsPriv, fileId, fileExt)

	dst, err := os.Create(file.Filename)
	if err != nil {
		c.Logger().Error("Failed to open destination file", err)
		return c.NoContent(fiber.StatusInternalServerError)
	}
	defer dst.Close()

	// Save file to storage
	if _, err = io.Copy(dst, src); err != nil {
		c.Logger().Errorf("Failed to save file: %v", err)
		return c.NoContent(fiber.StatusInternalServerError)
	}

	// business logic
	status, response, err := logic.CreateUploadChunck(*validation.Index, validation.SessionJwtToken, filePath)
	if err != nil {
		os.Remove(filePath)
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
