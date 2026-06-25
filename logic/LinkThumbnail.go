package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const maxLinkThumbnailUploadBytes int64 = 10 * 1024 * 1024

var safeThumbnailFileName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (s *Service) LinkThumbnailFilename(linkUUID string) string {
	return fmt.Sprintf("link-%s.webp", linkUUID)
}

func (s *Service) ResolvedThumbnailFilename(link models.Link) string {
	if link.Thumbnail != "" {
		return link.Thumbnail
	}
	return link.File.Thumbnail
}

func (s *Service) ResolvedThumbnailURL(link models.Link) string {
	return fmt.Sprintf(
		"%s/%s/image/thumb/%s",
		strings.TrimRight(s.Config().FolderVideoQualitysPub, "/"),
		link.UUID,
		s.ResolvedThumbnailFilename(link),
	)
}

func (s *Service) UpdateLinkThumbnail(linkID uint, userID uint, isAdmin bool, input io.Reader, fileSize int64, contentType string) (status int, err error) {
	dbLink, status, err := s.loadThumbnailLink(linkID, userID, isAdmin)
	if err != nil {
		return status, err
	}

	if fileSize <= 0 {
		return http.StatusBadRequest, errors.New("thumbnail is empty")
	}
	maxBytes := s.MaxLinkThumbnailUploadBytes()
	if fileSize > maxBytes {
		return http.StatusRequestEntityTooLarge, fmt.Errorf("exceeded max thumbnail filesize: %d", maxBytes)
	}
	if !s.allowedThumbnailContentType(contentType) {
		return http.StatusBadRequest, errors.New("thumbnail must be a JPEG, PNG, or WebP image")
	}

	outputFolder := filepath.Join(s.Config().FolderVideoQualitysPriv, dbLink.File.UUID)
	if err := os.MkdirAll(outputFolder, 0o777); err != nil {
		log.Printf("Failed to create thumbnail folder: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	tmpInput, err := os.CreateTemp(outputFolder, "thumbnail-input-*")
	if err != nil {
		log.Printf("Failed to create temporary thumbnail input: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	tmpInputPath := tmpInput.Name()
	defer os.Remove(tmpInputPath)

	written, err := io.Copy(tmpInput, io.LimitReader(input, maxBytes+1))
	closeErr := tmpInput.Close()
	if err != nil {
		log.Printf("Failed to write thumbnail input: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	if closeErr != nil {
		log.Printf("Failed to close thumbnail input: %v", closeErr)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	if written > maxBytes {
		return http.StatusRequestEntityTooLarge, fmt.Errorf("exceeded max thumbnail filesize: %d", maxBytes)
	}

	tmpOutput, err := os.CreateTemp(outputFolder, "thumbnail-output-*.webp")
	if err != nil {
		log.Printf("Failed to create temporary thumbnail output: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	tmpOutputPath := tmpOutput.Name()
	tmpOutput.Close()
	defer os.Remove(tmpOutputPath)

	if err := s.convertThumbnailToWebP(tmpInputPath, tmpOutputPath); err != nil {
		log.Printf("Failed to convert custom thumbnail for link %s: %v", dbLink.UUID, err)
		return http.StatusBadRequest, errors.New("failed to process thumbnail image")
	}

	thumbnailFileName := s.LinkThumbnailFilename(dbLink.UUID)
	finalPath := filepath.Join(outputFolder, thumbnailFileName)
	backupPath := ""
	if _, err := os.Stat(finalPath); err == nil {
		backupPath = filepath.Join(outputFolder, fmt.Sprintf(".%s.%s.bak", thumbnailFileName, uuid.NewString()))
		if err := os.Rename(finalPath, backupPath); err != nil {
			log.Printf("Failed to backup existing custom thumbnail: %v", err)
			return http.StatusInternalServerError, echo.ErrInternalServerError
		}
	}

	restoreBackup := func() {
		os.Remove(finalPath)
		if backupPath != "" {
			if err := os.Rename(backupPath, finalPath); err != nil {
				log.Printf("Failed to restore previous custom thumbnail: %v", err)
			}
		}
	}

	if err := os.Rename(tmpOutputPath, finalPath); err != nil {
		restoreBackup()
		log.Printf("Failed to promote custom thumbnail: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	if res := s.Deps.DB.Model(&dbLink).Update("thumbnail", thumbnailFileName); res.Error != nil {
		restoreBackup()
		log.Printf("Failed to save custom thumbnail: %v", res.Error)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	if backupPath != "" {
		os.Remove(backupPath)
	}

	return http.StatusOK, nil
}

func (s *Service) ResetLinkThumbnail(linkID uint, userID uint, isAdmin bool) (status int, err error) {
	dbLink, status, err := s.loadThumbnailLink(linkID, userID, isAdmin)
	if err != nil {
		return status, err
	}
	if dbLink.Thumbnail == "" {
		return http.StatusOK, nil
	}

	thumbnailPath := s.linkThumbnailPath(dbLink)
	if res := s.Deps.DB.Model(&dbLink).Update("thumbnail", ""); res.Error != nil {
		log.Printf("Failed to clear custom thumbnail: %v", res.Error)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	if err := os.Remove(thumbnailPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to delete custom thumbnail: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	return http.StatusOK, nil
}

func (s *Service) MaxLinkThumbnailUploadBytes() int64 {
	if s.Config().MaxPostSize > 0 && s.Config().MaxPostSize < maxLinkThumbnailUploadBytes {
		return s.Config().MaxPostSize
	}
	return maxLinkThumbnailUploadBytes
}

func (s *Service) RemoveLinkThumbnailFile(link models.Link) {
	if link.Thumbnail == "" {
		return
	}
	if err := os.Remove(s.linkThumbnailPath(link)); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to delete custom thumbnail for link %s: %v", link.UUID, err)
	}
}

func (s *Service) loadThumbnailLink(linkID uint, userID uint, isAdmin bool) (models.Link, int, error) {
	var dbLink models.Link
	if res := s.Deps.DB.
		Preload("File").
		First(&dbLink, linkID); res.Error != nil {
		return models.Link{}, http.StatusBadRequest, errors.New("file doesn't exist")
	}
	if !isAdmin && dbLink.UserID != userID {
		return models.Link{}, http.StatusForbidden, errors.New("unauthorized access to file")
	}
	return dbLink, http.StatusOK, nil
}

func (s *Service) linkThumbnailPath(link models.Link) string {
	return filepath.Join(s.Config().FolderVideoQualitysPriv, link.File.UUID, link.Thumbnail)
}

func (s *Service) convertThumbnailToWebP(inputPath string, outputPath string) error {
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", inputPath,
		"-vframes", "1",
		"-vf", "scale=w='min(1920,iw)':h='min(1080,ih)':force_original_aspect_ratio=decrease",
		"-q:v", "85",
		outputPath,
	)
	return cmd.Run()
}

func (s *Service) allowedThumbnailContentType(contentType string) bool {
	baseType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch baseType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func (s *Service) thumbnailFileAllowedForLink(fileName string, link models.Link) bool {
	if fileName == "" || strings.ContainsAny(fileName, `/\`) || !safeThumbnailFileName.MatchString(fileName) {
		return false
	}
	if fileName == link.File.Thumbnail {
		return true
	}
	return link.Thumbnail != "" && fileName == link.Thumbnail
}
