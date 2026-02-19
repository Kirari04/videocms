package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

func CreateApiKey(c echo.Context) error {
	isApiKey, _ := c.Get("IsApiKey").(bool)
	if isApiKey {
		return c.String(http.StatusForbidden, "API Key Not Permitted to Manage API Keys")
	}

	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.String(http.StatusInternalServerError, "Failed to catch UserID")
	}

	req := new(models.CreateApiKeyValidation)
	if status, err := helpers.Validate(c, req); err != nil {
		return c.String(status, err.Error())
	}

	status, apiKey, err := logic.GenerateApiKey(userID, req.Name, req.ExpiresAt)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, apiKey)
}

func ListApiKeys(c echo.Context) error {
	isApiKey, _ := c.Get("IsApiKey").(bool)
	if isApiKey {
		return c.String(http.StatusForbidden, "API Key Not Permitted to Manage API Keys")
	}

	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.String(http.StatusInternalServerError, "Failed to catch UserID")
	}

	status, apiKeys, err := logic.ListApiKeys(userID)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, apiKeys)
}

func DeleteApiKey(c echo.Context) error {
	isApiKey, _ := c.Get("IsApiKey").(bool)
	if isApiKey {
		return c.String(http.StatusForbidden, "API Key Not Permitted to Manage API Keys")
	}

	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.String(http.StatusInternalServerError, "Failed to catch UserID")
	}

	keyIDParam := c.Param("id")
	keyID, err := strconv.ParseUint(keyIDParam, 10, 32)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid API Key ID")
	}

	status, err := logic.DeleteApiKey(userID, uint(keyID))
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.NoContent(status)
}

func GetApiKeyAudit(c echo.Context) error {
	isApiKey, _ := c.Get("IsApiKey").(bool)
	if isApiKey {
		return c.String(http.StatusForbidden, "API Key Not Permitted to Manage API Keys")
	}

	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.String(http.StatusInternalServerError, "Failed to catch UserID")
	}

	keyIDParam := c.Param("id")
	keyID, err := strconv.ParseUint(keyIDParam, 10, 32)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid API Key ID")
	}

	status, logs, err := logic.GetApiKeyAudit(userID, uint(keyID))
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, logs)
}
