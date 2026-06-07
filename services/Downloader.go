package services

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	activeDownloadsMu     sync.Mutex
	activeDownloadCancels = map[uint]context.CancelFunc{}
)

func Downloader() {
	time.Sleep(time.Second * 5)
	resetStaleRemoteDownloads()

	for {
		if remoteDownloadsEnabled() {
			loadDownloadTasks()
		}
		time.Sleep(time.Second * 5)
	}
}

func loadDownloadTasks() {
	availableSlots := availableRemoteDownloadSlots()
	if availableSlots <= 0 {
		return
	}

	var pendingDownloads []models.RemoteDownload
	inits.DB.
		Where("status = ?", models.RemoteDownloadStatusPending).
		Order("created_at ASC").
		Limit(availableSlots).
		Find(&pendingDownloads)

	for _, download := range pendingDownloads {
		if availableRemoteDownloadSlots() <= 0 {
			return
		}
		if !remoteDownloadsEnabled() {
			return
		}

		now := time.Now()
		claimed := inits.DB.Model(&models.RemoteDownload{}).
			Where("id = ? AND status = ?", download.ID, models.RemoteDownloadStatusPending).
			Updates(map[string]interface{}{
				"status":     models.RemoteDownloadStatusDownloading,
				"started_at": &now,
				"error":      "",
				"progress":   0,
			})
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			continue
		}

		download.Status = models.RemoteDownloadStatusDownloading
		download.StartedAt = &now
		ctx, cancel := context.WithCancel(context.Background())
		registerActiveDownload(download.ID, cancel)

		go func(task models.RemoteDownload) {
			defer unregisterActiveDownload(task.ID)
			processDownload(ctx, task)
		}(download)
	}
}

func availableRemoteDownloadSlots() int {
	maxDownloads := 1
	if config.ENV.MaxParallelDownloads > 0 {
		maxDownloads = int(config.ENV.MaxParallelDownloads)
	}

	activeDownloadsMu.Lock()
	activeCount := len(activeDownloadCancels)
	activeDownloadsMu.Unlock()

	return maxDownloads - activeCount
}

func remoteDownloadsEnabled() bool {
	return config.ENV.RemoteDownloadEnabled == nil || *config.ENV.RemoteDownloadEnabled
}

func registerActiveDownload(id uint, cancel context.CancelFunc) {
	activeDownloadsMu.Lock()
	activeDownloadCancels[id] = cancel
	activeDownloadsMu.Unlock()
}

func unregisterActiveDownload(id uint) {
	activeDownloadsMu.Lock()
	delete(activeDownloadCancels, id)
	activeDownloadsMu.Unlock()
}

func activeRemoteDownloadIDs() map[uint]bool {
	activeDownloadsMu.Lock()
	defer activeDownloadsMu.Unlock()

	ids := make(map[uint]bool, len(activeDownloadCancels))
	for id := range activeDownloadCancels {
		ids[id] = true
	}
	return ids
}

func processDownload(parentCtx context.Context, task models.RemoteDownload) {
	if isCancelRequested(task.ID) {
		finishCanceled(&task, "Download canceled")
		return
	}
	if err := validateRemoteURLScheme(task.Url); err != nil {
		failDownload(&task, err.Error())
		return
	}

	downloadCtx := parentCtx
	cancelTimeout := func() {}
	if config.ENV.RemoteDownloadTimeout > 0 {
		downloadCtx, cancelTimeout = context.WithTimeout(parentCtx, time.Duration(config.ENV.RemoteDownloadTimeout)*time.Second)
	}
	defer cancelTimeout()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, task.Url, nil)
	if err != nil {
		failOrCancelDownload(&task, fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	resp, err := secureHTTPClient().Do(req)
	if err != nil {
		failOrCancelDownload(&task, fmt.Sprintf("Network error: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failDownload(&task, fmt.Sprintf("HTTP error: %s", resp.Status))
		return
	}

	totalSize := int64(0)
	if resp.ContentLength > 0 {
		totalSize = resp.ContentLength
	}
	if totalSize > 0 && config.ENV.MaxUploadFilesize > 0 && totalSize > config.ENV.MaxUploadFilesize {
		failDownload(&task, fmt.Sprintf("Remote file exceeds max upload size (%d bytes)", config.ENV.MaxUploadFilesize))
		return
	}

	fileName := resolveRemoteFileName(task, resp)
	tempPath := filepath.Join(config.ENV.FolderVideoUploadsPriv, fmt.Sprintf("remote_%d_%s.download", task.ID, uuid.NewString()))
	if err := os.MkdirAll(filepath.Dir(tempPath), 0755); err != nil {
		failDownload(&task, fmt.Sprintf("Failed to prepare upload folder: %v", err))
		return
	}

	task.Name = fileName
	task.TotalSize = totalSize
	task.TempPath = tempPath
	if res := inits.DB.Model(&task).Updates(map[string]interface{}{
		"name":       task.Name,
		"total_size": task.TotalSize,
		"temp_path":  task.TempPath,
	}); res.Error != nil {
		failDownload(&task, "Failed to persist download metadata")
		return
	}

	out, err := os.Create(tempPath)
	if err != nil {
		failDownload(&task, fmt.Sprintf("Failed to create temp file: %v", err))
		return
	}

	copied, copyErr := copyRemoteBody(downloadCtx, resp.Body, out, &task)
	closeErr := out.Close()
	if copyErr != nil {
		cleanupRemoteTemp(tempPath, fileName)
		failOrCancelDownload(&task, fmt.Sprintf("Download failed: %v", copyErr))
		return
	}
	if closeErr != nil {
		cleanupRemoteTemp(tempPath, fileName)
		failDownload(&task, fmt.Sprintf("Failed to close temp file: %v", closeErr))
		return
	}

	task.BytesDownloaded = copied
	if isCancelRequested(task.ID) {
		cleanupRemoteTemp(tempPath, fileName)
		finishCanceled(&task, "Download canceled")
		return
	}

	if res := inits.DB.Model(&task).Updates(map[string]interface{}{
		"status":           models.RemoteDownloadStatusImporting,
		"bytes_downloaded": task.BytesDownloaded,
		"progress":         0.95,
	}); res.Error != nil {
		cleanupRemoteTemp(tempPath, fileName)
		failDownload(&task, "Failed to update import status")
		return
	}
	task.Status = models.RemoteDownloadStatusImporting
	task.Progress = 0.95

	fileUUID := uuid.NewString()
	status, dbLink, cloned, err := logic.CreateFile(&tempPath, task.ParentFolderID, task.Name, fileUUID, task.BytesDownloaded, task.UserID, "")
	if err != nil {
		cleanupRemoteTemp(tempPath, fileName)
		failDownload(&task, fmt.Sprintf("Import failed (status %d): %v", status, err))
		return
	}
	if dbLink == nil {
		cleanupRemoteTemp(tempPath, fileName)
		failDownload(&task, "Import failed: missing created link")
		return
	}

	if isCancelRequested(task.ID) {
		_ = deleteImportedRemoteLink(task.UserID, dbLink.ID)
		if cloned {
			cleanupRemoteTemp(tempPath, fileName)
		}
		finishCanceled(&task, "Download canceled")
		return
	}

	if cloned {
		cleanupRemoteTemp(tempPath, fileName)
	}

	finishTime := time.Now()
	duration := 0.0
	if task.StartedAt != nil {
		duration = finishTime.Sub(*task.StartedAt).Seconds()
	}

	task.Status = models.RemoteDownloadStatusCompleted
	task.FinishedAt = &finishTime
	task.Progress = 1.0
	task.Duration = duration
	task.LinkID = dbLink.ID
	task.LinkUUID = dbLink.UUID
	task.FileID = dbLink.FileID
	task.Error = ""
	task.TempPath = ""
	inits.DB.Save(&task)

	logStats(task)
}

func secureHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			return dialPublicAddress(ctx, network, address)
		},
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return validateRemoteURLScheme(req.URL.String())
		},
	}
}

func dialPublicAddress(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for %s", host)
	}

	for _, ip := range ips {
		if isBlockedRemoteIP(ip) {
			return nil, fmt.Errorf("remote host resolves to blocked IP %s", ip.String())
		}
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func isBlockedRemoteIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func validateRemoteURLScheme(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("remote downloads only support http and https URLs")
	}
	if parsed.Host == "" {
		return errors.New("remote download URL is missing a host")
	}
	return nil
}

func resolveRemoteFileName(task models.RemoteDownload, resp *http.Response) string {
	if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
		_, params, err := mime.ParseMediaType(disposition)
		if err == nil {
			if filename := params["filename"]; filename != "" {
				return sanitizeRemoteFileName(filename, task.ID)
			}
			if filename := params["filename*"]; filename != "" {
				return sanitizeRemoteFileName(filename, task.ID)
			}
		}
	}

	if resp.Request != nil && resp.Request.URL != nil {
		if baseName := filepath.Base(resp.Request.URL.Path); baseName != "." && baseName != "/" && baseName != "" {
			if decoded, err := url.PathUnescape(baseName); err == nil {
				return sanitizeRemoteFileName(decoded, task.ID)
			}
			return sanitizeRemoteFileName(baseName, task.ID)
		}
	}

	return sanitizeRemoteFileName(fmt.Sprintf("remote-download-%d", task.ID), task.ID)
}

func sanitizeRemoteFileName(name string, id uint) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " ._")
	if name == "" {
		name = fmt.Sprintf("remote-download-%d", id)
	}

	const maxNameLength = 128
	runes := []rune(name)
	if len(runes) <= maxNameLength {
		return name
	}

	ext := filepath.Ext(name)
	extRunes := []rune(ext)
	if ext != "" && len(extRunes) < maxNameLength-8 {
		baseRunes := []rune(strings.TrimSuffix(name, ext))
		allowedBaseLength := maxNameLength - len(extRunes)
		if len(baseRunes) > allowedBaseLength {
			baseRunes = baseRunes[:allowedBaseLength]
		}
		return strings.TrimRight(string(baseRunes), " ._") + ext
	}
	return string(runes[:maxNameLength])
}

func copyRemoteBody(ctx context.Context, src io.Reader, dst io.Writer, task *models.RemoteDownload) (int64, error) {
	buffer := make([]byte, 64*1024)
	var downloaded int64
	lastUpdate := time.Now()

	for {
		if err := ctx.Err(); err != nil {
			return downloaded, err
		}

		n, readErr := src.Read(buffer)
		if n > 0 {
			downloaded += int64(n)
			if config.ENV.MaxUploadFilesize > 0 && downloaded > config.ENV.MaxUploadFilesize {
				return downloaded, fmt.Errorf("remote file exceeds max upload size (%d bytes)", config.ENV.MaxUploadFilesize)
			}
			if _, err := dst.Write(buffer[:n]); err != nil {
				return downloaded, err
			}

			if time.Since(lastUpdate) > time.Second {
				task.BytesDownloaded = downloaded
				if task.TotalSize > 0 {
					task.Progress = float64(downloaded) / float64(task.TotalSize)
				}
				inits.DB.Model(task).Updates(map[string]interface{}{
					"bytes_downloaded": task.BytesDownloaded,
					"progress":         task.Progress,
				})
				lastUpdate = time.Now()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				task.BytesDownloaded = downloaded
				if task.TotalSize > 0 {
					task.Progress = float64(downloaded) / float64(task.TotalSize)
				}
				inits.DB.Model(task).Updates(map[string]interface{}{
					"bytes_downloaded": task.BytesDownloaded,
					"progress":         task.Progress,
				})
				return downloaded, nil
			}
			return downloaded, readErr
		}
	}
}

func failOrCancelDownload(task *models.RemoteDownload, reason string) {
	if isCancelRequested(task.ID) {
		cleanupRemoteTemp(task.TempPath, task.Name)
		finishCanceled(task, "Download canceled")
		return
	}
	failDownload(task, reason)
}

func failDownload(task *models.RemoteDownload, reason string) {
	now := time.Now()
	task.Status = models.RemoteDownloadStatusFailed
	task.Error = truncateRemoteError(reason)
	task.FinishedAt = &now
	if task.StartedAt != nil {
		task.Duration = now.Sub(*task.StartedAt).Seconds()
	}
	task.TempPath = ""
	inits.DB.Save(task)
}

func finishCanceled(task *models.RemoteDownload, reason string) {
	now := time.Now()
	task.Status = models.RemoteDownloadStatusCanceled
	task.Error = truncateRemoteError(reason)
	task.FinishedAt = &now
	task.CanceledAt = &now
	task.Progress = 0
	if task.StartedAt != nil {
		task.Duration = now.Sub(*task.StartedAt).Seconds()
	}
	task.TempPath = ""
	inits.DB.Save(task)
}

func isCancelRequested(id uint) bool {
	var current models.RemoteDownload
	if res := inits.DB.Select("status").First(&current, id); res.Error != nil {
		return false
	}
	return current.Status == models.RemoteDownloadStatusCanceling || current.Status == models.RemoteDownloadStatusCanceled
}

func cleanupRemoteTemp(tempPath string, fileName string) {
	if tempPath == "" {
		return
	}
	_ = os.Remove(tempPath)
	if ext := filepath.Ext(fileName); ext != "" {
		_ = os.Remove(tempPath + ext)
	}
}

func deleteImportedRemoteLink(userID uint, linkID uint) error {
	if linkID == 0 {
		return nil
	}
	_, err := logic.DeleteFiles(&models.LinksDeleteValidation{
		LinkIDs: []models.LinkDeleteValidation{{LinkID: linkID}},
	}, userID, false)
	return err
}

func truncateRemoteError(reason string) string {
	const maxErrorLength = 1024
	if len(reason) <= maxErrorLength {
		return reason
	}
	return reason[:maxErrorLength]
}

func CancelRemoteDownload(userID uint, downloadID uint) (int, error) {
	var task models.RemoteDownload
	if res := inits.DB.Where("id = ? AND user_id = ?", downloadID, userID).First(&task); res.Error != nil {
		return http.StatusNotFound, errors.New("download not found")
	}

	if models.IsRemoteDownloadTerminal(task.Status) {
		return http.StatusConflict, errors.New("download is already finished")
	}

	now := time.Now()
	switch task.Status {
	case models.RemoteDownloadStatusPending:
		task.Status = models.RemoteDownloadStatusCanceled
		task.Error = "Download canceled"
		task.CancelRequestedAt = &now
		task.CanceledAt = &now
		task.FinishedAt = &now
		if res := inits.DB.Save(&task); res.Error != nil {
			return http.StatusInternalServerError, errors.New("failed to cancel download")
		}
		return http.StatusOK, nil
	default:
		if res := inits.DB.Model(&task).Updates(map[string]interface{}{
			"status":              models.RemoteDownloadStatusCanceling,
			"error":               "Cancellation requested",
			"cancel_requested_at": &now,
		}); res.Error != nil {
			return http.StatusInternalServerError, errors.New("failed to request cancellation")
		}

		activeDownloadsMu.Lock()
		cancel := activeDownloadCancels[task.ID]
		activeDownloadsMu.Unlock()
		if cancel != nil {
			cancel()
			return http.StatusOK, nil
		}

		cleanupRemoteTemp(task.TempPath, task.Name)
		task.Status = models.RemoteDownloadStatusCanceled
		task.Error = "Download canceled"
		task.CanceledAt = &now
		task.FinishedAt = &now
		task.TempPath = ""
		inits.DB.Save(&task)
		return http.StatusOK, nil
	}
}

func CancelAllRemoteDownloads(reason string) {
	now := time.Now()
	activeIDsMap := activeRemoteDownloadIDs()
	activeIDs := make([]uint, 0, len(activeIDsMap))
	for id := range activeIDsMap {
		activeIDs = append(activeIDs, id)
	}

	if len(activeIDs) > 0 {
		inits.DB.Model(&models.RemoteDownload{}).
			Where("id IN ? AND status IN ?", activeIDs, []string{
				models.RemoteDownloadStatusDownloading,
				models.RemoteDownloadStatusImporting,
				models.RemoteDownloadStatusCanceling,
			}).
			Updates(map[string]interface{}{
				"status":              models.RemoteDownloadStatusCanceling,
				"error":               truncateRemoteError(reason),
				"cancel_requested_at": &now,
			})

		activeDownloadsMu.Lock()
		for _, cancel := range activeDownloadCancels {
			cancel()
		}
		activeDownloadsMu.Unlock()
	}

	cancelQuery := inits.DB.Model(&models.RemoteDownload{}).
		Where("status = ?", models.RemoteDownloadStatusPending)
	if len(activeIDs) > 0 {
		cancelQuery = cancelQuery.Or("status IN ? AND id NOT IN ?", []string{
			models.RemoteDownloadStatusDownloading,
			models.RemoteDownloadStatusImporting,
			models.RemoteDownloadStatusCanceling,
		}, activeIDs)
	} else {
		cancelQuery = cancelQuery.Or("status IN ?", []string{
			models.RemoteDownloadStatusDownloading,
			models.RemoteDownloadStatusImporting,
			models.RemoteDownloadStatusCanceling,
		})
	}
	cancelQuery.Updates(map[string]interface{}{
		"status":              models.RemoteDownloadStatusCanceled,
		"error":               truncateRemoteError(reason),
		"cancel_requested_at": &now,
		"canceled_at":         &now,
		"finished_at":         &now,
		"temp_path":           "",
	})
}

func resetStaleRemoteDownloads() {
	var staleDownloads []models.RemoteDownload
	if res := inits.DB.Where("status IN ?", []string{
		models.RemoteDownloadStatusDownloading,
		models.RemoteDownloadStatusImporting,
		models.RemoteDownloadStatusCanceling,
	}).Find(&staleDownloads); res.Error != nil {
		log.Printf("Failed to load stale remote downloads: %v", res.Error)
		return
	}

	now := time.Now()
	for _, task := range staleDownloads {
		cleanupRemoteTemp(task.TempPath, task.Name)
		if task.Status == models.RemoteDownloadStatusCanceling {
			task.Status = models.RemoteDownloadStatusCanceled
			task.Error = "Download canceled during server restart"
			task.CanceledAt = &now
		} else {
			task.Status = models.RemoteDownloadStatusFailed
			task.Error = "Download interrupted by server restart"
		}
		task.FinishedAt = &now
		task.TempPath = ""
		inits.DB.Save(&task)
	}
}

func logStats(task models.RemoteDownload) {
	domain := "unknown"
	u, err := url.Parse(task.Url)
	if err == nil && u.Host != "" {
		domain = u.Host
	} else {
		parts := strings.Split(task.Url, "//")
		if len(parts) > 1 {
			domain = strings.Split(parts[1], "/")[0]
		}
	}

	stat := models.RemoteDownloadLog{
		UserID:  task.UserID,
		Domain:  domain,
		Bytes:   uint64(task.BytesDownloaded),
		Seconds: task.Duration,
	}
	inits.DB.Create(&stat)
}
