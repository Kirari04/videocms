package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func parseUploadStatsRequest(c echo.Context) (from time.Time, to time.Time, points int, validatus models.UploadStatsGetValidation, err error) {
	if _, err := helpers.Validate(c, &validatus); err != nil {
		return from, to, points, validatus, err
	}

	to = time.Now()
	if validatus.To != "" {
		if t, err := time.Parse(time.RFC3339, validatus.To); err == nil {
			to = t
		}
	}

	if validatus.From != "" {
		if t, err := time.Parse(time.RFC3339, validatus.From); err == nil {
			from = t
		}
	}

	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}

	if validatus.Points > 0 {
		points = validatus.Points
	} else {
		points = 100
	}
	return from, to, points, validatus, nil
}

func GetUploadStats(c echo.Context) error {
	from, to, points, _, err := parseUploadStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	userID := c.Get("UserID").(uint)

	stats, err := logic.GetUploadStats(from, to, points, userID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}

func GetAdminUploadStats(c echo.Context) error {
	from, to, points, validatus, err := parseUploadStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	stats, err := logic.GetUploadStats(from, to, points, validatus.UserID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}
