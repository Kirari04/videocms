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

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func CreateFile(c echo.Context) error {
	if !*config.ENV.UploadEnabled {
		return c.String(http.StatusServiceUnavailable, "Upload has been desabled")
	}

	// parse & validate request
	var fileValidation models.FileCreateValidation
	if status, err := helpers.Validate(c, &fileValidation); err != nil {
		return c.String(status, err.Error())
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.String(http.StatusBadRequest, "No File uploaded")
	}
	src, err := file.Open()
	if err != nil {
		c.Logger().Error("Failed to open src file", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer src.Close()

	fileId := uuid.NewString()
	fileSplit := strings.Split(file.Filename, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	filePath := fmt.Sprintf("%s/%s.%s", config.ENV.FolderVideoUploadsPriv, fileId, fileExt)

	// Save file to storage
	dst, err := os.Create(filePath)
	if err != nil {
		c.Logger().Error("Failed to open destination file", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer dst.Close()
	if _, err = io.Copy(dst, src); err != nil {
		c.Logger().Errorf("Failed to save file: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	// business logic
	status, dbLink, cloned, err := logic.CreateFile(&filePath, fileValidation.ParentFolderID, file.Filename, fileId, file.Size, c.Get("UserID").(uint), "")
	if err != nil || cloned {
		os.Remove(filePath)
	}
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, dbLink)
}
