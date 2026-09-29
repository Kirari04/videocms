package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

func (s *Service) DeleteFolders(folderValidation *models.FoldersDeleteValidation, userID uint, isAdmin bool) (status int, err error) {
	if !s.Deps.RequestGate.TryStart(userID) {
		return http.StatusTooManyRequests, errors.New("wait until the previous delete request finished")
	}
	defer s.Deps.RequestGate.End(userID)

	if len(folderValidation.FolderIDs) == 0 {
		return http.StatusBadRequest, errors.New("array FolderIDs is empty")
	}
	if int64(len(folderValidation.FolderIDs)) > s.Config().MaxItemsMultiDelete {
		return http.StatusBadRequest, errors.New("max requested items exceeded")
	}

	var links []models.Link
	status = http.StatusInternalServerError
	err = s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		folders, collectedLinks, collectionStatus, err := collectFolderDeletion(tx, folderValidation.FolderIDs, userID, isAdmin)
		if err != nil {
			status = collectionStatus
			return err
		}
		links = collectedLinks
		// Preserve the existing descendant-link limit independently of the
		// selected-folder limit.
		if int64(len(links)) > s.Config().MaxItemsMultiDelete {
			status = http.StatusBadRequest
			return errors.New("max requested items exceeded")
		}
		if deleteStatus, err := deleteLinksInTransaction(tx, links, userID, isAdmin); err != nil {
			status = deleteStatus
			return err
		}
		query := tx
		if !isAdmin {
			query = query.Where("user_id = ?", userID)
		}
		result := query.Delete(&models.Folder{}, folders)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(folders)) {
			status = http.StatusForbidden
			return errors.New("folder ownership changed during deletion")
		}
		return nil
	})
	if err != nil {
		return status, err
	}
	for _, link := range links {
		s.RemoveLinkThumbnailFile(link)
	}
	return http.StatusOK, nil
}

// collectFolderDeletion visits every live descendant, including legacy mixed
// ownership trees. Filtering descendants by owner would strand their data.
func collectFolderDeletion(tx *gorm.DB, roots []models.FolderDeleteValidation, userID uint, isAdmin bool) ([]uint, []models.Link, int, error) {
	visited := make(map[uint]bool, len(roots))
	frontier := make([]uint, 0, len(roots))
	var parentFolderID uint
	for i, root := range roots {
		if visited[root.FolderID] {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("the folders have to be distinct. Folder %d is dublicate", root.FolderID)
		}
		var folder models.Folder
		err := tx.First(&folder, root.FolderID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && !isAdmin && folder.UserID != userID) {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("FolderID (%d) doesn't exist", root.FolderID)
		}
		if err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		if i == 0 {
			parentFolderID = folder.ParentFolderID
		} else if folder.ParentFolderID != parentFolderID {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("all folders have to share the same parent folder. Folder %d doesnt: %d (required) vs %d (actual)", root.FolderID, parentFolderID, folder.ParentFolderID)
		}
		visited[folder.ID] = true
		frontier = append(frontier, folder.ID)
	}

	var folders []uint
	var links []models.Link
	for len(frontier) > 0 {
		folders = append(folders, frontier...)
		var children []models.Folder
		if err := tx.Where("parent_folder_id IN ?", frontier).Find(&children).Error; err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		var childLinks []models.Link
		if err := tx.Preload("File").Where("parent_folder_id IN ?", frontier).Find(&childLinks).Error; err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		for _, link := range childLinks {
			if !isAdmin && link.UserID != userID {
				return nil, nil, http.StatusForbidden, errors.New("folder contains a file owned by another user")
			}
		}
		links = append(links, childLinks...)
		frontier = nil
		for _, child := range children {
			if !isAdmin && child.UserID != userID {
				return nil, nil, http.StatusForbidden, errors.New("folder contains a folder owned by another user")
			}
			if !visited[child.ID] {
				visited[child.ID] = true
				frontier = append(frontier, child.ID)
			}
		}
	}
	return folders, links, http.StatusOK, nil
}
