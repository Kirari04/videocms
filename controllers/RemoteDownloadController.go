package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func CreateRemoteDownload(c echo.Context) error {
	userId := c.Get("UserID").(uint)

	// parse & validate request
	var req models.RemoteDownloadRequest
	if status, err := helpers.Validate(c, &req); err != nil {
		return c.String(status, err.Error())
	}

	// Check user limits
	user, err := helpers.GetUser(userId)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to fetch user")
	}

	maxDownloads := user.Settings.MaxRemoteDownloads
	if maxDownloads == 0 {
		maxDownloads = 5 // Default limit if not set
	}

	var currentCount int64
	inits.DB.Model(&models.RemoteDownload{}).
		Where("user_id = ? AND status IN ?", userId, []string{"pending", "downloading"}).
		Count(&currentCount)

	if currentCount+int64(len(req.Urls)) > int64(maxDownloads) {
		return c.String(http.StatusTooManyRequests, "Queue limit reached")
	}

	// Create records
	var created []*models.RemoteDownload
	for _, url := range req.Urls {
		download := models.RemoteDownload{
			UserID:         userId,
			ParentFolderID: req.ParentFolderID,
			Url:            url,
			Status:         "pending",
			Progress:       0,
		}
		if res := inits.DB.Create(&download); res.Error != nil {
			return c.String(http.StatusInternalServerError, "Failed to create download task")
		}
		created = append(created, &download)
	}

	return c.JSON(http.StatusCreated, created)
}

func ListRemoteDownloads(c echo.Context) error {
	userId := c.Get("UserID").(uint)

	var downloads []models.RemoteDownload
	if res := inits.DB.Where("user_id = ?", userId).Order("created_at DESC").Limit(50).Find(&downloads); res.Error != nil {
		return c.String(http.StatusInternalServerError, "Failed to fetch downloads")
	}

	return c.JSON(http.StatusOK, downloads)
}

func GetRemoteDownloadStats(c echo.Context) error {
	var validation models.RemoteDownloadStatsGetValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	var from, to time.Time
	var err error

	if validation.From == "" {
		from = time.Now().AddDate(0, 0, -30) // Default 30 days ago
	} else {
		from, err = time.Parse(time.RFC3339, validation.From)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid from date (use RFC3339)")
		}
	}

	if validation.To == "" {
		to = time.Now().Add(24 * time.Hour)
	} else {
		to, err = time.Parse(time.RFC3339, validation.To)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid to date (use RFC3339)")
		}
	}

	if validation.Points <= 0 {
		validation.Points = 20
	}

	stats, err := logic.GetRemoteDownloadStats(from, to, validation.Points, c.Get("UserID").(uint))
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func GetRemoteDownloadDurationStats(c echo.Context) error {
	var validation models.RemoteDownloadStatsGetValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	var from, to time.Time
	var err error

	if validation.From == "" {
		from = time.Now().AddDate(0, 0, -30)
	} else {
		from, err = time.Parse(time.RFC3339, validation.From)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid from date (use RFC3339)")
		}
	}

	if validation.To == "" {
		to = time.Now().Add(24 * time.Hour)
	} else {
		to, err = time.Parse(time.RFC3339, validation.To)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid to date (use RFC3339)")
		}
	}

	if validation.Points <= 0 {
		validation.Points = 20
	}

	stats, err := logic.GetRemoteDownloadDurationStats(from, to, validation.Points, c.Get("UserID").(uint))
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func GetTopRemoteDownloadStats(c echo.Context) error {
	var validation models.RemoteDownloadStatsGetValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	var from, to time.Time
	var err error

	if validation.From == "" {
		from = time.Now().AddDate(0, 0, -30)
	} else {
		from, err = time.Parse(time.RFC3339, validation.From)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid from date (use RFC3339)")
		}
	}

	if validation.To == "" {
		to = time.Now().Add(24 * time.Hour)
	} else {
		to, err = time.Parse(time.RFC3339, validation.To)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid to date (use RFC3339)")
		}
	}

	mode := c.QueryParam("mode")
	if mode == "" {
		mode = "domains"
	}

	stats, err := logic.GetTopRemoteDownloadTraffic(from, to, c.Get("UserID").(uint), 10, mode)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func GetAdminRemoteDownloadStats(c echo.Context) error {
	var validation models.RemoteDownloadStatsGetValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	var from, to time.Time
	var err error

	if validation.From == "" {
		from = time.Now().AddDate(0, 0, -30)
	} else {
		from, err = time.Parse(time.RFC3339, validation.From)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid from date (use RFC3339)")
		}
	}

	if validation.To == "" {
		to = time.Now().Add(24 * time.Hour)
	} else {
		to, err = time.Parse(time.RFC3339, validation.To)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid to date (use RFC3339)")
		}
	}

	if validation.Points <= 0 {
		validation.Points = 20
	}

	stats, err := logic.GetRemoteDownloadStats(from, to, validation.Points, validation.UserID)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func GetAdminRemoteDownloadDurationStats(c echo.Context) error {
	var validation models.RemoteDownloadStatsGetValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	var from, to time.Time
	var err error

	if validation.From == "" {
		from = time.Now().AddDate(0, 0, -30)
	} else {
		from, err = time.Parse(time.RFC3339, validation.From)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid from date (use RFC3339)")
		}
	}

	if validation.To == "" {
		to = time.Now().Add(24 * time.Hour)
	} else {
		to, err = time.Parse(time.RFC3339, validation.To)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid to date (use RFC3339)")
		}
	}

	if validation.Points <= 0 {
		validation.Points = 20
	}

	stats, err := logic.GetRemoteDownloadDurationStats(from, to, validation.Points, validation.UserID)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func GetAdminTopRemoteDownloadStats(c echo.Context) error {
	var validation models.RemoteDownloadStatsGetValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	var from, to time.Time
	var err error

	if validation.From == "" {
		from = time.Now().AddDate(0, 0, -30)
	} else {
		from, err = time.Parse(time.RFC3339, validation.From)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid from date (use RFC3339)")
		}
	}

	if validation.To == "" {
		to = time.Now().Add(24 * time.Hour)
	} else {
		to, err = time.Parse(time.RFC3339, validation.To)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid to date (use RFC3339)")
		}
	}

	mode := c.QueryParam("mode")
	if mode == "" {
		mode = "domains"
	}

	stats, err := logic.GetTopRemoteDownloadTraffic(from, to, validation.UserID, 10, mode)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}
