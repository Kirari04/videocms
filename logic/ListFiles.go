package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// LinkListItem is the row shape returned by the file list and search
// endpoints. It carries enough metadata to render a library row without
// a follow-up per-file request.
type LinkListItem struct {
	ID             uint
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
	UUID           string
	Name           string
	ParentFolderID uint
	Tags           []*models.Tag
	Size           int64
	Duration       float64
	Thumbnail      string
	Processing     bool
	Available      bool
}

func (s *Service) buildLinkListItems(links []models.Link) *[]LinkListItem {
	items := make([]LinkListItem, 0, len(links))
	for _, link := range links {
		processing := false
		for _, quality := range link.File.Qualitys {
			if !quality.Ready && !quality.Failed {
				processing = true
				break
			}
		}
		available := link.File.StorageState != models.FileStorageUnavailable
		if !available {
			processing = false
		}
		items = append(items, LinkListItem{
			ID:             link.ID,
			CreatedAt:      link.CreatedAt,
			UpdatedAt:      link.UpdatedAt,
			UUID:           link.UUID,
			Name:           link.Name,
			ParentFolderID: link.ParentFolderID,
			Tags:           link.Tags,
			Size:           link.File.Size,
			Duration:       link.File.Duration,
			Thumbnail:      s.ResolvedThumbnailURL(link),
			Processing:     processing,
			Available:      available,
		})
	}
	return &items
}

func (s *Service) ListFiles(fromFolder uint, userId uint) (status int, response *[]LinkListItem, err error) {
	//check if requested folder exists
	if fromFolder > 0 {
		res := s.Deps.DB.First(&models.Folder{}, fromFolder)
		if res.Error != nil {
			return http.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// query all files
	var links []models.Link
	res := s.Deps.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Preload("File.Qualitys").
		Preload("Tags").
		Where(&models.Link{
			ParentFolderID: fromFolder,
			UserID:         userId,
		}, "ParentFolderID", "UserID").
		Order("name ASC").
		Find(&links)
	if res.Error != nil {
		log.Printf("Failed to query file list: %v", res.Error)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	return http.StatusOK, s.buildLinkListItems(links), nil
}
