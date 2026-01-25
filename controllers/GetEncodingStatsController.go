package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func parseEncodingStatsRequest(c echo.Context) (from time.Time, to time.Time, points int, validatus models.EncodingStatsGetValidation, err error) {
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

func GetEncodingStats(c echo.Context) error {
	from, to, points, _, err := parseEncodingStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	userID := c.Get("UserID").(uint)

	stats, err := logic.GetEncodingStats(from, to, points, userID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}

func GetAdminEncodingStats(c echo.Context) error {
	from, to, points, validatus, err := parseEncodingStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	stats, err := logic.GetEncodingStats(from, to, points, validatus.UserID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}

func GetTopEncodingStats(c echo.Context) error {
	from, to, _, _, err := parseEncodingStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	userID := c.Get("UserID").(uint)
	limit := 10
	results, err := logic.GetTopEncoding(from, to, userID, limit, "files")
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}

func GetAdminTopEncodingStats(c echo.Context) error {
	from, to, _, validatus, err := parseEncodingStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	mode := c.QueryParam("mode")
	if mode == "" {
		mode = "users"
	}

	limit := 10
	results, err := logic.GetTopEncoding(from, to, validatus.UserID, limit, mode)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}
