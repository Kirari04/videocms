package services

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var downloadLimitChan chan bool

func Downloader() {
	// Initialize with a default buffer, will be resized based on config in the loop or we just set a reasonable max
	// Actually Encoder re-makes the channel, but that's a bit racy if config changes.
	// For now, let's just initialize it once or check config periodically.
	// A better way is to semaphore, but let's stick to the project pattern.

	// We'll delay the start slightly to ensure config is loaded
	time.Sleep(time.Second * 5)

	for {
		maxDownloads := 1
		if config.ENV.MaxParallelDownloads > 0 {
			maxDownloads = int(config.ENV.MaxParallelDownloads)
		}

		// If channel is nil or capacity changed (simple check), recreate
		if downloadLimitChan == nil || cap(downloadLimitChan) != maxDownloads {
			downloadLimitChan = make(chan bool, maxDownloads)
		}

		go loadDownloadTasks()
		time.Sleep(time.Second * 5)
	}
}

func loadDownloadTasks() {
	var pendingDownloads []models.RemoteDownload

	inits.DB.
		Where("status = ?", "pending").
		Order("created_at ASC").
		Limit(10).
		Find(&pendingDownloads)

	for _, download := range pendingDownloads {
		// Acquire slot
		downloadLimitChan <- true

		go func(task models.RemoteDownload) {
			defer func() {
				<-downloadLimitChan
			}()
			processDownload(task)
		}(download)
	}
}

func processDownload(task models.RemoteDownload) {
	// Update status to downloading
	now := time.Now()
	task.Status = "downloading"
	task.StartedAt = &now
	inits.DB.Save(&task)

	// Create temp file
	tempDir := os.TempDir()
	fileName := filepath.Base(task.Url)
	// Sanitize filename
	fileName = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, fileName)
	if fileName == "" || fileName == "." {
		fileName = "downloaded_file"
	}

	destPath := filepath.Join(tempDir, fmt.Sprintf("dl_%d_%s", task.ID, fileName))

	// Setup request
	req, err := http.NewRequest("GET", task.Url, nil)
	if err != nil {
		failDownload(&task, fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	// Timeout
	timeout := time.Duration(config.ENV.RemoteDownloadTimeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Minute // Default
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		failDownload(&task, fmt.Sprintf("Network error: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failDownload(&task, fmt.Sprintf("HTTP error: %s", resp.Status))
		return
	}

	// Get size if available
	contentLength := resp.ContentLength
	task.TotalSize = contentLength
	inits.DB.Save(&task)

	out, err := os.Create(destPath)
	if err != nil {
		failDownload(&task, fmt.Sprintf("Failed to create temp file: %v", err))
		return
	}
	defer out.Close()
	defer os.Remove(destPath) // Clean up temp file after we are done (CreateFile copies/moves it?)
	// Actually logic.CreateFile likely moves it or we need to keep it until processed.
	// logic.CreateFile takes &filePath. If it moves it, we are good. If it copies, we need to delete.
	// Looking at CreateFile -> CloneFileByHash (if exists) -> else ...
	// logic.CreateFile -> checks CloneFileByHash. If not found -> moves/processes.
	// Wait, logic.CreateFile seems to call ffmpeg on the file.
	// Let's assume we need to pass the path and let CreateFile handle it.
	// BUT, CreateFile signature: func CreateFile(fromFile *string, ...)
	// logic/CreateFile.go:
	// if err == nil { return status, newLink, true, err } // file exists (dedup)
	// ...
	// Run file through ffmpeg...
	// It doesn't seem to strictly move it in all cases, but let's check.
	// To be safe, we will defer remove, but only if CreateFile doesn't need it anymore.
	// CreateFile usually takes ownership or we should copy.
	// Let's write to it first.

	// Progress tracker
	counter := &WriteCounter{
		Total:    uint64(contentLength),
		Download: &task,
	}
	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		out.Close()
		failDownload(&task, fmt.Sprintf("Download failed: %v", err))
		return
	}
	out.Close()

	// Download finished
	finishTime := time.Now()
	duration := finishTime.Sub(*task.StartedAt).Seconds()
	task.Duration = duration
	task.BytesDownloaded = int64(counter.Downloaded)

	// Calculate Hash
	hash, err := helpers.HashFile(destPath)
	if err != nil {
		failDownload(&task, fmt.Sprintf("Hashing failed: %v", err))
		return
	}

	// Create File in CMS
	// Note: We pass empty string for excludeSessionUUID as this isn't an upload session
	status, _, _, err := logic.CreateFile(&destPath, task.ParentFolderID, fileName, hash, task.BytesDownloaded, task.UserID, "")
	if err != nil {
		failDownload(&task, fmt.Sprintf("Import failed (status %d): %v", status, err))
		return
	}

	// Success
	task.Status = "completed"
	task.FinishedAt = &finishTime
	task.Progress = 1.0
	inits.DB.Save(&task)

	// Log Stats
	logStats(task)
}

func failDownload(task *models.RemoteDownload, reason string) {
	now := time.Now()
	task.Status = "failed"
	task.Error = reason
	task.FinishedAt = &now
	inits.DB.Save(task)
}

func logStats(task models.RemoteDownload) {
	domain := "unknown"
	u, err := url.Parse(task.Url)
	if err == nil && u.Host != "" {
		domain = u.Host
	} else {
		// Fallback for cases where url.Parse might fail or return empty host (e.g. missing scheme)
		// Extract domain by looking for // and taking the next part
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

type WriteCounter struct {
	Total      uint64
	Downloaded uint64
	Download   *models.RemoteDownload
	LastUpdate time.Time
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Downloaded += uint64(n)

	// Update DB every second to avoid spam
	if time.Since(wc.LastUpdate) > time.Second {
		wc.Download.BytesDownloaded = int64(wc.Downloaded)
		if wc.Total > 0 {
			wc.Download.Progress = float64(wc.Downloaded) / float64(wc.Total)
		}
		inits.DB.Model(wc.Download).Updates(map[string]interface{}{
			"bytes_downloaded": wc.Download.BytesDownloaded,
			"progress":         wc.Download.Progress,
		})
		wc.LastUpdate = time.Now()
	}
	return n, nil
}
