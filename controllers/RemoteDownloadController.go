package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handlers) CreateRemoteDownload(c echo.Context) error {
	userId := c.Get("UserID").(uint)

	if !h.globalRemoteDownloadsEnabled() {
		return c.String(http.StatusServiceUnavailable, "Remote downloads are disabled")
	}

	// parse & validate request
	var req models.RemoteDownloadRequest
	if status, err := helpers.Validate(c, &req); err != nil {
		return c.String(status, err.Error())
	}

	// Check user limits
	user, err := h.Logic.GetModelUser(userId)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to fetch user")
	}
	if !user.Settings.EffectiveRemoteDownloadEnabled() {
		return c.String(http.StatusForbidden, "Remote downloads are disabled for this user")
	}
	if err := validateRemoteDownloadURLs(req.Urls); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if req.ParentFolderID > 0 {
		var folder models.Folder
		if res := h.Deps.DB.Where("id = ? AND user_id = ?", req.ParentFolderID, userId).First(&folder); res.Error != nil {
			return c.String(http.StatusBadRequest, "Parent folder not found")
		}
	}

	maxDownloads := user.Settings.EffectiveMaxRemoteDownloads()

	var currentCount int64
	h.Deps.DB.Model(&models.RemoteDownload{}).
		Where("user_id = ? AND status IN ?", userId, models.ActiveRemoteDownloadStatuses()).
		Count(&currentCount)

	if currentCount+int64(len(req.Urls)) > int64(maxDownloads) {
		return c.String(http.StatusTooManyRequests, "Queue limit reached")
	}

	// Create records
	var created []models.RemoteDownload
	if err := h.Deps.DB.Transaction(func(tx *gorm.DB) error {
		for _, rawURL := range req.Urls {
			download := models.RemoteDownload{
				UserID:         userId,
				ParentFolderID: req.ParentFolderID,
				Url:            strings.TrimSpace(rawURL),
				Status:         models.RemoteDownloadStatusPending,
				Progress:       0,
			}
			if res := tx.Create(&download); res.Error != nil {
				return res.Error
			}
			created = append(created, download)
		}
		return nil
	}); err != nil {
		return c.String(http.StatusInternalServerError, "Failed to create download task")
	}

	return c.JSON(http.StatusCreated, created)
}

func (h *Handlers) ListRemoteDownloads(c echo.Context) error {
	userId := c.Get("UserID").(uint)

	var downloads []models.RemoteDownload
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}

	query := h.Deps.DB.Where("user_id = ?", userId)
	status := c.QueryParam("status")
	if status != "" {
		if !isKnownRemoteDownloadStatus(status) {
			return c.String(http.StatusBadRequest, "Invalid status")
		}
		query = query.Where("status = ?", status)
	}

	if res := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&downloads); res.Error != nil {
		return c.String(http.StatusInternalServerError, "Failed to fetch downloads")
	}

	return c.JSON(http.StatusOK, downloads)
}

func (h *Handlers) CancelRemoteDownload(c echo.Context) error {
	downloadID, err := parseRemoteDownloadID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid download ID")
	}

	status, err := h.Workers.CancelRemoteDownload(c.Get("UserID").(uint), downloadID)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.String(http.StatusOK, "ok")
}

func (h *Handlers) RetryRemoteDownload(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	downloadID, err := parseRemoteDownloadID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid download ID")
	}

	download, status, err := h.retryRemoteDownload(userID, downloadID)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.JSON(status, download)
}

func (h *Handlers) DeleteRemoteDownload(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	downloadID, err := parseRemoteDownloadID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid download ID")
	}

	var download models.RemoteDownload
	if res := h.Deps.DB.Where("id = ? AND user_id = ?", downloadID, userID).First(&download); res.Error != nil {
		return c.String(http.StatusNotFound, "download not found")
	}
	if !models.IsRemoteDownloadTerminal(download.Status) {
		return c.String(http.StatusConflict, "active downloads cannot be deleted")
	}
	if res := h.Deps.DB.Delete(&download); res.Error != nil {
		return c.String(http.StatusInternalServerError, "failed to delete download")
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handlers) ClearRemoteDownloads(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	var req models.RemoteDownloadClearRequest
	if status, err := helpers.Validate(c, &req); err != nil {
		return c.String(status, err.Error())
	}

	seen := map[string]bool{}
	for _, status := range req.Statuses {
		if !models.IsRemoteDownloadTerminal(status) {
			return c.String(http.StatusBadRequest, "only terminal statuses can be cleared")
		}
		if seen[status] {
			return c.String(http.StatusBadRequest, "statuses must be distinct")
		}
		seen[status] = true
	}

	res := h.Deps.DB.Where("user_id = ? AND status IN ?", userID, req.Statuses).Delete(&models.RemoteDownload{})
	if res.Error != nil {
		return c.String(http.StatusInternalServerError, "failed to clear downloads")
	}
	return c.JSON(http.StatusOK, echo.Map{"deleted": res.RowsAffected})
}

func parseRemoteDownloadID(c echo.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	return uint(id), err
}

func (h *Handlers) retryRemoteDownload(userID uint, downloadID uint) (*models.RemoteDownload, int, error) {
	if !h.globalRemoteDownloadsEnabled() {
		return nil, http.StatusServiceUnavailable, errors.New("remote downloads are disabled")
	}

	user, err := h.Logic.GetModelUser(userID)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to fetch user")
	}
	if !user.Settings.EffectiveRemoteDownloadEnabled() {
		return nil, http.StatusForbidden, errors.New("remote downloads are disabled for this user")
	}

	var download models.RemoteDownload
	if res := h.Deps.DB.Where("id = ? AND user_id = ?", downloadID, userID).First(&download); res.Error != nil {
		return nil, http.StatusNotFound, errors.New("download not found")
	}
	if download.Status != models.RemoteDownloadStatusFailed && download.Status != models.RemoteDownloadStatusCanceled {
		return nil, http.StatusConflict, errors.New("only failed or canceled downloads can be retried")
	}

	var currentCount int64
	h.Deps.DB.Model(&models.RemoteDownload{}).
		Where("user_id = ? AND status IN ?", userID, models.ActiveRemoteDownloadStatuses()).
		Count(&currentCount)
	if currentCount+1 > int64(user.Settings.EffectiveMaxRemoteDownloads()) {
		return nil, http.StatusTooManyRequests, errors.New("queue limit reached")
	}
	if err := validateRemoteDownloadURLs([]string{download.Url}); err != nil {
		return nil, http.StatusBadRequest, err
	}
	if download.ParentFolderID > 0 {
		var folder models.Folder
		if res := h.Deps.DB.Where("id = ? AND user_id = ?", download.ParentFolderID, userID).First(&folder); res.Error != nil {
			return nil, http.StatusBadRequest, errors.New("parent folder not found")
		}
	}

	removeRemoteDownloadTemp(download.TempPath, download.Name)

	if res := h.Deps.DB.Model(&download).Updates(map[string]interface{}{
		"status":              models.RemoteDownloadStatusPending,
		"progress":            0,
		"error":               "",
		"temp_path":           "",
		"link_id":             0,
		"link_uuid":           "",
		"file_id":             0,
		"bytes_downloaded":    0,
		"total_size":          0,
		"duration":            0,
		"started_at":          nil,
		"finished_at":         nil,
		"cancel_requested_at": nil,
		"canceled_at":         nil,
	}); res.Error != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to retry download")
	}

	if res := h.Deps.DB.First(&download, download.ID); res.Error != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to load retried download")
	}
	return &download, http.StatusOK, nil
}

func removeRemoteDownloadTemp(tempPath string, fileName string) {
	if tempPath == "" {
		return
	}
	_ = os.Remove(tempPath)
	if ext := filepath.Ext(fileName); ext != "" {
		_ = os.Remove(tempPath + ext)
	}
}

func (h *Handlers) globalRemoteDownloadsEnabled() bool {
	return h.Config().RemoteDownloadEnabled == nil || *h.Config().RemoteDownloadEnabled
}

func validateRemoteDownloadURLs(urls []string) error {
	for _, rawURL := range urls {
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil {
			return fmt.Errorf("invalid URL: %s", rawURL)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return errors.New("remote downloads only support http and https URLs")
		}
		if parsed.Host == "" {
			return errors.New("remote download URL is missing a host")
		}
	}
	return nil
}

func isKnownRemoteDownloadStatus(status string) bool {
	switch status {
	case models.RemoteDownloadStatusPending,
		models.RemoteDownloadStatusDownloading,
		models.RemoteDownloadStatusImporting,
		models.RemoteDownloadStatusCompleted,
		models.RemoteDownloadStatusFailed,
		models.RemoteDownloadStatusCanceling,
		models.RemoteDownloadStatusCanceled:
		return true
	default:
		return false
	}
}

func (h *Handlers) GetRemoteDownloadStats(c echo.Context) error {
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

	stats, err := h.Logic.GetRemoteDownloadStats(from, to, validation.Points, c.Get("UserID").(uint))
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetRemoteDownloadDurationStats(c echo.Context) error {
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

	stats, err := h.Logic.GetRemoteDownloadDurationStats(from, to, validation.Points, c.Get("UserID").(uint))
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetTopRemoteDownloadStats(c echo.Context) error {
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

	stats, err := h.Logic.GetTopRemoteDownloadTraffic(from, to, c.Get("UserID").(uint), 10, mode)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetAdminRemoteDownloadStats(c echo.Context) error {
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

	stats, err := h.Logic.GetRemoteDownloadStats(from, to, validation.Points, validation.UserID)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetAdminRemoteDownloadDurationStats(c echo.Context) error {
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

	stats, err := h.Logic.GetRemoteDownloadDurationStats(from, to, validation.Points, validation.UserID)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}

func (h *Handlers) GetAdminTopRemoteDownloadStats(c echo.Context) error {
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

	stats, err := h.Logic.GetTopRemoteDownloadTraffic(from, to, validation.UserID, 10, mode)
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, stats)
}
