package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"github.com/labstack/echo/v4"
	"net/http"
	"time"
)

func GetSystemStats(c echo.Context) error {
	var validatus models.SystemResourceGetValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	to := time.Now()
	var from time.Time
	var points int

	// 1. Parse 'from' and 'to' from query
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

	// 2. Handle Legacy Interval (only if 'from' not set)
	if from.IsZero() && validatus.Interval != "" {
		switch validatus.Interval {
		case "5min":
			from = to.Add(-4 * time.Hour)
		case "1h":
			from = to.Add(-24 * time.Hour)
		case "7h":
			from = to.Add(-168 * time.Hour) // 7 days
		}
	}

	// 3. Default 'from' if still missing (Default to 24h)
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}

	// 4. Determine Points
	if validatus.Points > 0 {
		points = validatus.Points
	} else {
		// Smart Interval Calculation
		// Goal: ~10-15 min intervals for < 24h
		// Goal: Avoid too many points (max ~200)

		duration := to.Sub(from)
		minutes := duration.Minutes()

		if minutes <= 1440 { // <= 24 hours
			// Target 15 minute intervals -> 96 points for 24h
			// Min points 50
			calcPoints := int(minutes / 15)
			if calcPoints < 50 {
				points = 50
			} else if calcPoints > 200 {
				points = 200
			} else {
				points = calcPoints
			}
		} else {
			// For longer ranges, stick to ~100 points
			points = 100
		}
	}

	// 5. Fetch Data
	stats, err := logic.GetSystemStats(from, to, points)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}
