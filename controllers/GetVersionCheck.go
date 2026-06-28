package controllers

import (
	"ch/kirari04/videocms/config"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/imroc/req/v3"
	"github.com/labstack/echo/v4"
)

type githubLatestRelease struct {
	TagName string `json:"tag_name"`
}

func (h *Handlers) GetVersionCheck(c echo.Context) error {

	client := req.C()
	res, err := client.R().
		SetContext(c.Request().Context()).
		Get("https://api.github.com/repos/Kirari04/videocms/releases/latest")
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("Error fetching version: %v", err.Error()))
	}
	if !res.IsSuccessState() {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("Error fetching version with status %d: %v", res.StatusCode, res.String()))
	}

	var latestRelease githubLatestRelease
	if err := json.Unmarshal([]byte(res.String()), &latestRelease); err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("Error parsing version response: %v", err.Error()))
	}

	newestVersion := strings.TrimSpace(latestRelease.TagName)
	currentVersion := strings.TrimSpace(config.VERSION)

	if newestVersion == "" {
		return c.String(http.StatusInternalServerError, "Error fetching version: latest release tag is empty")
	}

	if currentVersion != newestVersion {
		return c.String(http.StatusBadRequest, fmt.Sprintf("A new version is available: %s. Current version: %s", newestVersion, currentVersion))
	}

	return c.String(http.StatusOK, fmt.Sprintf("You are using the latest version: %s", currentVersion))
}
