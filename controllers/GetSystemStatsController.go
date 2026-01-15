package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func GetSystemStats(c echo.Context) error {
	var validatus models.SystemResourceGetValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	to := time.Now()
	from := to.Add(-24 * time.Hour) // Default 24h
	points := 50

	// Handle Legacy Interval
	if validatus.Interval != "" {
		switch validatus.Interval {
		case "5min":
			from = to.Add(-4 * time.Hour)
			points = 48
		case "1h":
			from = to.Add(-24 * time.Hour)
			points = 24
		case "7h":
			from = to.Add(-168 * time.Hour) // 7 days
			points = 24
		}
	}

	// Handle Custom Range (overrides Interval)
	if validatus.From != "" {
		if t, err := time.Parse(time.RFC3339, validatus.From); err == nil {
			from = t
		}
	}
	if validatus.To != "" {
		if t, err := time.Parse(time.RFC3339, validatus.To); err == nil {
			to = t
		}
	}
	if validatus.Points > 0 {
		points = validatus.Points
	}

	stats, err := logic.GetSystemStats(from, to, points)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}