package logic

import (
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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

	store, layout, err := s.mediaStorage(dbLink.File.StorageID)
	if err != nil {
		log.Printf("Failed to resolve thumbnail storage: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	ctx := context.Background()
	workspace := s.Deps.Storage.Workspace()
	if workspace == nil {
		log.Printf("Failed to resolve thumbnail workspace")
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	tmpInput, cleanupInput, err := workspace.TempFile(ctx, "thumbnail-input", "")
	if err != nil {
		log.Printf("Failed to create temporary thumbnail input: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	tmpInputPath := tmpInput.Name()
	defer cleanupInput()

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

	tmpOutput, cleanupOutput, err := workspace.TempFile(ctx, "thumbnail-output", ".webp")
	if err != nil {
		log.Printf("Failed to create temporary thumbnail output: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	tmpOutputPath := tmpOutput.Name()
	tmpOutput.Close()
	defer cleanupOutput()

	if err := s.convertThumbnailToWebP(tmpInputPath, tmpOutputPath); err != nil {
		log.Printf("Failed to convert custom thumbnail for link %s: %v", dbLink.UUID, err)
		return http.StatusBadRequest, errors.New("failed to process thumbnail image")
	}

	thumbnailFileName := fmt.Sprintf("link-%s-%s.webp", dbLink.UUID, uuid.NewString())
	thumbnailKey, err := layout.Thumbnail(dbLink.File.UUID, thumbnailFileName)
	if err != nil {
		log.Printf("Failed to build custom thumbnail key: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	output, err := os.Open(tmpOutputPath)
	if err != nil {
		log.Printf("Failed to open converted custom thumbnail: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	outputInfo, err := output.Stat()
	if err != nil {
		output.Close()
		log.Printf("Failed to inspect converted custom thumbnail: %v", err)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	expectedSize := outputInfo.Size()
	_, putErr := store.Put(ctx, thumbnailKey, output, storage.PutOptions{
		ContentType:  "image/webp",
		CacheControl: "public, max-age=31536000, immutable",
		ExpectedSize: &expectedSize,
	})
	closeErr = output.Close()
	if putErr != nil || closeErr != nil {
		log.Printf("Failed to publish custom thumbnail: put=%v close=%v", putErr, closeErr)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	previousThumbnail := dbLink.Thumbnail
	if res := s.Deps.DB.Model(&dbLink).Update("thumbnail", thumbnailFileName); res.Error != nil {
		_ = store.Delete(ctx, thumbnailKey)
		log.Printf("Failed to save custom thumbnail: %v", res.Error)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	if previousThumbnail != "" {
		previousKey, keyErr := layout.Thumbnail(dbLink.File.UUID, previousThumbnail)
		if keyErr != nil {
			log.Printf("Failed to build previous custom thumbnail key: %v", keyErr)
		} else if deleteErr := store.Delete(ctx, previousKey); deleteErr != nil {
			log.Printf("Failed to delete previous custom thumbnail: %v", deleteErr)
		}
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

	store, layout, storageErr := s.mediaStorage(dbLink.File.StorageID)
	if storageErr != nil {
		log.Printf("Failed to resolve thumbnail storage: %v", storageErr)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	ctx := context.Background()
	thumbnailKey, keyErr := layout.Thumbnail(dbLink.File.UUID, dbLink.Thumbnail)
	if keyErr != nil {
		log.Printf("Failed to build custom thumbnail key: %v", keyErr)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}
	previousThumbnail := dbLink.Thumbnail
	if res := s.Deps.DB.Model(&dbLink).Update("thumbnail", ""); res.Error != nil {
		log.Printf("Failed to clear custom thumbnail: %v", res.Error)
		return http.StatusInternalServerError, echo.ErrInternalServerError
	}

	if err := store.Delete(ctx, thumbnailKey); err != nil {
		if restoreErr := s.Deps.DB.Model(&dbLink).Update("thumbnail", previousThumbnail).Error; restoreErr != nil {
			log.Printf("Failed to restore custom thumbnail reference after delete failure: %v", restoreErr)
		}
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
	store, layout, err := s.mediaStorage(link.File.StorageID)
	if err != nil {
		log.Printf("Failed to resolve custom thumbnail storage for link %s: %v", link.UUID, err)
		return
	}
	ctx := context.Background()
	key, err := layout.Thumbnail(link.File.UUID, link.Thumbnail)
	if err != nil {
		log.Printf("Failed to build custom thumbnail key for link %s: %v", link.UUID, err)
		return
	}
	if err := store.Delete(ctx, key); err != nil {
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
