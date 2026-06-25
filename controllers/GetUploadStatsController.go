package controllers

import (
	"ch/kirari04/videocms/helpers"
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

func (h *Handlers) GetUploadStats(c echo.Context) error {
	from, to, points, _, err := parseUploadStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	userID := c.Get("UserID").(uint)

	stats, err := h.Logic.GetUploadStats(from, to, points, userID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetAdminUploadStats(c echo.Context) error {
	from, to, points, validatus, err := parseUploadStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	stats, err := h.Logic.GetUploadStats(from, to, points, validatus.UserID)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetTopUploadStats(c echo.Context) error {
	from, to, _, _, err := parseUploadStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	mode := c.QueryParam("mode")
	if mode == "" {
		mode = "files"
	}

	userID := c.Get("UserID").(uint)
	limit := 10
	results, err := h.Logic.GetTopUpload(from, to, userID, limit, mode)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}

func (h *Handlers) GetAdminTopUploadStats(c echo.Context) error {
	from, to, _, validatus, err := parseUploadStatsRequest(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	mode := c.QueryParam("mode")
	if mode == "" {
		mode = "users"
	}

	limit := 10
	results, err := h.Logic.GetTopUpload(from, to, validatus.UserID, limit, mode)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}
