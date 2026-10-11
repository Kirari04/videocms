package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

// Leave room for ownership and soft-delete parameters on databases with a
// limited number of parameters per statement.
const deletionBatchSize = 500

func (s *Service) DeleteFiles(fileValidation *models.LinksDeleteValidation, userID uint, isAdmin bool) (status int, err error) {
	if len(fileValidation.LinkIDs) == 0 {
		return http.StatusBadRequest, errors.New("array LinkIDs is empty")
	}
	if int64(len(fileValidation.LinkIDs)) > s.Config().MaxItemsMultiDelete {
		return http.StatusBadRequest, errors.New("max requested items exceeded")
	}
	var links []models.Link
	status = http.StatusInternalServerError
	err = s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		seen := make(map[uint]bool, len(fileValidation.LinkIDs))
		for _, requested := range fileValidation.LinkIDs {
			if seen[requested.LinkID] {
				status = http.StatusBadRequest
				return fmt.Errorf("the files have to be distinct. File %d is dublicate", requested.LinkID)
			}
			var link models.Link
			err := tx.Preload("File").First(&link, requested.LinkID).Error
			if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && !isAdmin && link.UserID != userID) {
				status = http.StatusBadRequest
				return fmt.Errorf("linkID (%d) doesn't exist", requested.LinkID)
			}
			if err != nil {
				return err
			}
			seen[link.ID] = true
			links = append(links, link)
		}
		var err error
		deleteStatus, err := deleteLinksInTransaction(tx, links, userID, isAdmin)
		if err != nil {
			status = deleteStatus
		}
		return err
	})
	if err != nil {
		return status, err
	}
	for _, link := range links {
		s.RemoveLinkThumbnailFile(link)
	}
	return http.StatusOK, nil
}

func deleteLinksInTransaction(tx *gorm.DB, links []models.Link, userID uint, isAdmin bool) (int, error) {
	if len(links) == 0 {
		return http.StatusOK, nil
	}
	ids := make([]uint, 0, len(links))
	for _, link := range links {
		if !isAdmin && link.UserID != userID {
			return http.StatusForbidden, errors.New("file belongs to another user")
		}
		ids = append(ids, link.ID)
	}
	for start := 0; start < len(ids); start += deletionBatchSize {
		batch := ids[start:min(start+deletionBatchSize, len(ids))]
		query := tx
		if !isAdmin {
			query = query.Where("user_id = ?", userID)
		}
		result := query.Delete(&models.Link{}, batch)
		if result.Error != nil {
			return http.StatusInternalServerError, result.Error
		}
		if result.RowsAffected != int64(len(batch)) {
			return http.StatusForbidden, errors.New("file ownership changed during deletion")
		}
	}
	return http.StatusOK, nil
}
