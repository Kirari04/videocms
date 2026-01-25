package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func parseTrafficStatsRequest(c echo.Context) (from time.Time, to time.Time, points int, validatus models.TrafficStatsGetValidation, err error) {
	if _, err := helpers.Validate(c, &validatus); err != nil {
		return from, to, points, validatus, err
	}

	to = time.Now()
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
		duration := to.Sub(from)
		minutes := duration.Minutes()
		if minutes <= 1440 { // <= 24 hours
			calcPoints := int(minutes / 15)
			if calcPoints < 50 {
				points = 50
			} else if calcPoints > 200 {
				points = 200
			} else {
				points = calcPoints
			}
		} else {
			points = 100
		}
	}
	return from, to, points, validatus, nil
}

func GetTrafficStats(c echo.Context) error {

	from, to, points, validatus, err := parseTrafficStatsRequest(c)

	if err != nil {

		return c.String(http.StatusBadRequest, err.Error())

	}



	userID := c.Get("UserID").(uint)



	stats, err := logic.GetTrafficStats(from, to, points, userID, validatus.FileID, validatus.QualityID)

	if err != nil {

		return c.NoContent(http.StatusInternalServerError)

	}



	return c.JSON(http.StatusOK, stats)
}

func GetAdminTrafficStats(c echo.Context) error {
	from, to, points, validatus, err := parseTrafficStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	stats, err := logic.GetTrafficStats(from, to, points, validatus.UserID, validatus.FileID, validatus.QualityID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}

func GetTopTrafficStats(c echo.Context) error {
	from, to, _, _, err := parseTrafficStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	userID := c.Get("UserID").(uint)

	limit := 10
	results, err := logic.GetTopTraffic(from, to, userID, limit, "files")
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, results)
}

func GetAdminTopTrafficStats(c echo.Context) error {
	from, to, _, validatus, err := parseTrafficStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	mode := c.QueryParam("mode") // "users" or "files"
	if mode == "" {
		mode = "users"
	}

	limit := 10
	results, err := logic.GetTopTraffic(from, to, validatus.UserID, limit, mode)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, results)
}

