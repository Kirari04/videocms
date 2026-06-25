package services

import (
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
	"time"
	"unicode"

	"github.com/google/uuid"
)

func (w *WorkerGroup) Downloader(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !sleepContext(ctx, time.Second*5) {
		return
	}
	w.resetStaleRemoteDownloads()

	for {
		if w.remoteDownloadsEnabled() {
			w.loadDownloadTasks(ctx)
		}
		if !sleepContext(ctx, time.Second*5) {
			return
		}
	}
}

func (w *WorkerGroup) loadDownloadTasks(ctx context.Context) {
	availableSlots := w.availableRemoteDownloadSlots()
	if availableSlots <= 0 {
		return
	}

	var pendingDownloads []models.RemoteDownload
	w.deps.DB.
		Where("status = ?", models.RemoteDownloadStatusPending).
		Order("created_at ASC").
		Limit(availableSlots).
		Find(&pendingDownloads)

	for _, download := range pendingDownloads {
		if ctx.Err() != nil {
			return
		}
		if w.availableRemoteDownloadSlots() <= 0 {
			return
		}
		if !w.remoteDownloadsEnabled() {
			return
		}

		now := time.Now()
		claimed := w.deps.DB.Model(&models.RemoteDownload{}).
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
		downloadCtx, cancel := context.WithCancel(ctx)
		w.registerActiveDownload(download.ID, cancel)

		go func(task models.RemoteDownload) {
			defer w.unregisterActiveDownload(task.ID)
			w.processDownload(downloadCtx, task)
		}(download)
	}
}

func (w *WorkerGroup) availableRemoteDownloadSlots() int {
	maxDownloads := 1
	cfg := w.Config()
	if cfg.MaxParallelDownloads > 0 {
		maxDownloads = int(cfg.MaxParallelDownloads)
	}

	w.activeDownloadsMu.Lock()
	activeCount := len(w.activeDownloadCancels)
	w.activeDownloadsMu.Unlock()

	return maxDownloads - activeCount
}

func (w *WorkerGroup) remoteDownloadsEnabled() bool {
	cfg := w.Config()
	return cfg.RemoteDownloadEnabled == nil || *cfg.RemoteDownloadEnabled
}

func (w *WorkerGroup) registerActiveDownload(id uint, cancel context.CancelFunc) {
	w.activeDownloadsMu.Lock()
	w.activeDownloadCancels[id] = cancel
	w.activeDownloadsMu.Unlock()
}

func (w *WorkerGroup) unregisterActiveDownload(id uint) {
	w.activeDownloadsMu.Lock()
	delete(w.activeDownloadCancels, id)
	w.activeDownloadsMu.Unlock()
}

func (w *WorkerGroup) activeRemoteDownloadIDs() map[uint]bool {
	w.activeDownloadsMu.Lock()
	defer w.activeDownloadsMu.Unlock()

	ids := make(map[uint]bool, len(w.activeDownloadCancels))
	for id := range w.activeDownloadCancels {
		ids[id] = true
	}
	return ids
}

func (w *WorkerGroup) processDownload(parentCtx context.Context, task models.RemoteDownload) {
	if w.isCancelRequested(task.ID) {
		w.finishCanceled(&task, "Download canceled")
		return
	}
	if err := validateRemoteURLScheme(task.Url); err != nil {
		w.failDownload(&task, err.Error())
		return
	}

	downloadCtx := parentCtx
	cancelTimeout := func() {}
	cfg := w.Config()
	if cfg.RemoteDownloadTimeout > 0 {
		downloadCtx, cancelTimeout = context.WithTimeout(parentCtx, time.Duration(cfg.RemoteDownloadTimeout)*time.Second)
	}
	defer cancelTimeout()

	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, task.Url, nil)
	if err != nil {
		w.failOrCancelDownload(&task, fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	resp, err := secureHTTPClient().Do(req)
	if err != nil {
		w.failOrCancelDownload(&task, fmt.Sprintf("Network error: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.failDownload(&task, fmt.Sprintf("HTTP error: %s", resp.Status))
		return
	}

	totalSize := int64(0)
	if resp.ContentLength > 0 {
		totalSize = resp.ContentLength
	}
	cfg = w.Config()
	if totalSize > 0 && cfg.MaxUploadFilesize > 0 && totalSize > cfg.MaxUploadFilesize {
		w.failDownload(&task, fmt.Sprintf("Remote file exceeds max upload size (%d bytes)", cfg.MaxUploadFilesize))
		return
	}

	fileName := resolveRemoteFileName(task, resp)
	tempPath := filepath.Join(cfg.FolderVideoUploadsPriv, fmt.Sprintf("remote_%d_%s.download", task.ID, uuid.NewString()))
	if err := os.MkdirAll(filepath.Dir(tempPath), 0755); err != nil {
		w.failDownload(&task, fmt.Sprintf("Failed to prepare upload folder: %v", err))
		return
	}

	task.Name = fileName
	task.TotalSize = totalSize
	task.TempPath = tempPath
	if res := w.deps.DB.Model(&task).Updates(map[string]interface{}{
		"name":       task.Name,
		"total_size": task.TotalSize,
		"temp_path":  task.TempPath,
	}); res.Error != nil {
		w.failDownload(&task, "Failed to persist download metadata")
		return
	}

	out, err := os.Create(tempPath)
	if err != nil {
		w.failDownload(&task, fmt.Sprintf("Failed to create temp file: %v", err))
		return
	}

	copied, copyErr := w.copyRemoteBody(downloadCtx, resp.Body, out, &task)
	closeErr := out.Close()
	if copyErr != nil {
		cleanupRemoteTemp(tempPath, fileName)
		w.failOrCancelDownload(&task, fmt.Sprintf("Download failed: %v", copyErr))
		return
	}
	if closeErr != nil {
		cleanupRemoteTemp(tempPath, fileName)
		w.failDownload(&task, fmt.Sprintf("Failed to close temp file: %v", closeErr))
		return
	}

	task.BytesDownloaded = copied
	if w.isCancelRequested(task.ID) {
		cleanupRemoteTemp(tempPath, fileName)
		w.finishCanceled(&task, "Download canceled")
		return
	}

	if res := w.deps.DB.Model(&task).Updates(map[string]interface{}{
		"status":           models.RemoteDownloadStatusImporting,
		"bytes_downloaded": task.BytesDownloaded,
		"progress":         0.95,
	}); res.Error != nil {
		cleanupRemoteTemp(tempPath, fileName)
		w.failDownload(&task, "Failed to update import status")
		return
	}
	task.Status = models.RemoteDownloadStatusImporting
	task.Progress = 0.95

	fileUUID := uuid.NewString()
	status, dbLink, cloned, err := w.logic.CreateFile(&tempPath, task.ParentFolderID, task.Name, fileUUID, task.BytesDownloaded, task.UserID, "")
	if err != nil {
		cleanupRemoteTemp(tempPath, fileName)
		w.failDownload(&task, fmt.Sprintf("Import failed (status %d): %v", status, err))
		return
	}
	if dbLink == nil {
		cleanupRemoteTemp(tempPath, fileName)
		w.failDownload(&task, "Import failed: missing created link")
		return
	}

	if w.isCancelRequested(task.ID) {
		_ = w.deleteImportedRemoteLink(task.UserID, dbLink.ID)
		if cloned {
			cleanupRemoteTemp(tempPath, fileName)
		}
		w.finishCanceled(&task, "Download canceled")
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
	w.deps.DB.Save(&task)

	w.logStats(task)
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

func (w *WorkerGroup) copyRemoteBody(ctx context.Context, src io.Reader, dst io.Writer, task *models.RemoteDownload) (int64, error) {
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
			cfg := w.Config()
			if cfg.MaxUploadFilesize > 0 && downloaded > cfg.MaxUploadFilesize {
				return downloaded, fmt.Errorf("remote file exceeds max upload size (%d bytes)", cfg.MaxUploadFilesize)
			}
			if _, err := dst.Write(buffer[:n]); err != nil {
				return downloaded, err
			}

			if time.Since(lastUpdate) > time.Second {
				task.BytesDownloaded = downloaded
				if task.TotalSize > 0 {
					task.Progress = float64(downloaded) / float64(task.TotalSize)
				}
				w.deps.DB.Model(task).Updates(map[string]interface{}{
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
				w.deps.DB.Model(task).Updates(map[string]interface{}{
					"bytes_downloaded": task.BytesDownloaded,
					"progress":         task.Progress,
				})
				return downloaded, nil
			}
			return downloaded, readErr
		}
	}
}

func (w *WorkerGroup) failOrCancelDownload(task *models.RemoteDownload, reason string) {
	if w.isCancelRequested(task.ID) {
		cleanupRemoteTemp(task.TempPath, task.Name)
		w.finishCanceled(task, "Download canceled")
		return
	}
	w.failDownload(task, reason)
}

func (w *WorkerGroup) failDownload(task *models.RemoteDownload, reason string) {
	now := time.Now()
	task.Status = models.RemoteDownloadStatusFailed
	task.Error = truncateRemoteError(reason)
	task.FinishedAt = &now
	if task.StartedAt != nil {
		task.Duration = now.Sub(*task.StartedAt).Seconds()
	}
	task.TempPath = ""
	w.deps.DB.Save(task)
}

func (w *WorkerGroup) finishCanceled(task *models.RemoteDownload, reason string) {
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
	w.deps.DB.Save(task)
}

func (w *WorkerGroup) isCancelRequested(id uint) bool {
	var current models.RemoteDownload
	if res := w.deps.DB.Select("status").First(&current, id); res.Error != nil {
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

func (w *WorkerGroup) deleteImportedRemoteLink(userID uint, linkID uint) error {
	if linkID == 0 {
		return nil
	}
	_, err := w.logic.DeleteFiles(&models.LinksDeleteValidation{
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

func (w *WorkerGroup) CancelRemoteDownload(userID uint, downloadID uint) (int, error) {
	var task models.RemoteDownload
	if res := w.deps.DB.Where("id = ? AND user_id = ?", downloadID, userID).First(&task); res.Error != nil {
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
		if res := w.deps.DB.Save(&task); res.Error != nil {
			return http.StatusInternalServerError, errors.New("failed to cancel download")
		}
		return http.StatusOK, nil
	default:
		if res := w.deps.DB.Model(&task).Updates(map[string]interface{}{
			"status":              models.RemoteDownloadStatusCanceling,
			"error":               "Cancellation requested",
			"cancel_requested_at": &now,
		}); res.Error != nil {
			return http.StatusInternalServerError, errors.New("failed to request cancellation")
		}

		w.activeDownloadsMu.Lock()
		cancel := w.activeDownloadCancels[task.ID]
		w.activeDownloadsMu.Unlock()
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
		w.deps.DB.Save(&task)
		return http.StatusOK, nil
	}
}

func (w *WorkerGroup) CancelAllRemoteDownloads(reason string) {
	now := time.Now()
	activeIDsMap := w.activeRemoteDownloadIDs()
	activeIDs := make([]uint, 0, len(activeIDsMap))
	for id := range activeIDsMap {
		activeIDs = append(activeIDs, id)
	}

	if len(activeIDs) > 0 {
		w.deps.DB.Model(&models.RemoteDownload{}).
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

		w.activeDownloadsMu.Lock()
		for _, cancel := range w.activeDownloadCancels {
			cancel()
		}
		w.activeDownloadsMu.Unlock()
	}

	cancelQuery := w.deps.DB.Model(&models.RemoteDownload{}).
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

func (w *WorkerGroup) resetStaleRemoteDownloads() {
	var staleDownloads []models.RemoteDownload
	if res := w.deps.DB.Where("status IN ?", []string{
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
		w.deps.DB.Save(&task)
	}
}

func (w *WorkerGroup) logStats(task models.RemoteDownload) {
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
	w.deps.DB.Create(&stat)
}
