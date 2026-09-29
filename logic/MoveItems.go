package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"

	"gorm.io/gorm"
)

func (s *Service) MoveItems(userId uint, targetFolderId uint, folderIds []uint, linkIds []uint, isAdmin bool) (int, error) {
	if len(folderIds) == 0 && len(linkIds) == 0 {
		return http.StatusBadRequest, errors.New("no items selected to move")
	}

	status := http.StatusInternalServerError
	err := s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		fail := func(code int, message string) error {
			status = code
			return errors.New(message)
		}

		var targetFolderOwnerID uint
		if targetFolderId > 0 {
			var targetFolder models.Folder
			if res := tx.First(&targetFolder, targetFolderId); res.Error != nil {
				return fail(http.StatusBadRequest, "target folder doesn't exist")
			}
			if !isAdmin && targetFolder.UserID != userId {
				return fail(http.StatusForbidden, "unauthorized access to target folder")
			}
			targetFolderOwnerID = targetFolder.UserID
		}

		// Validate the entire selection before changing any parent. Using the same
		// transaction for traversal and updates also serializes opposing SQLite moves.
		for _, folderId := range folderIds {
			if folderId == targetFolderId {
				return fail(http.StatusBadRequest, "cannot move folder into itself")
			}

			var folder models.Folder
			if res := tx.First(&folder, folderId); res.Error != nil {
				return fail(http.StatusBadRequest, "folder to move not found")
			}
			if !isAdmin && folder.UserID != userId {
				return fail(http.StatusForbidden, "unauthorized access to folder")
			}
			if targetFolderId > 0 && folder.UserID != targetFolderOwnerID {
				return fail(http.StatusBadRequest, "cannot move folder to a destination owned by another user")
			}

			if targetFolderId > 0 {
				contains, err := folderContainsFolder(tx, folderId, targetFolderId)
				if err != nil {
					return err
				}
				if contains {
					return fail(http.StatusBadRequest, "cannot move parent folder into its child")
				}
			}
		}

		for _, linkId := range linkIds {
			var link models.Link
			if res := tx.First(&link, linkId); res.Error != nil {
				return fail(http.StatusBadRequest, "file to move not found")
			}
			if !isAdmin && link.UserID != userId {
				return fail(http.StatusForbidden, "unauthorized access to file")
			}
			if targetFolderId > 0 && link.UserID != targetFolderOwnerID {
				return fail(http.StatusBadRequest, "cannot move file to a destination owned by another user")
			}
		}

		for _, folderId := range folderIds {
			if err := tx.Model(&models.Folder{}).Where("id = ?", folderId).Update("parent_folder_id", targetFolderId).Error; err != nil {
				return err
			}
		}
		for _, linkId := range linkIds {
			if err := tx.Model(&models.Link{}).Where("id = ?", linkId).Update("parent_folder_id", targetFolderId).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return status, err
	}
	return http.StatusOK, nil
}

func (s *Service) FolderContainsFolder(folderID uint, searchingFolderID uint) (bool, error) {
	return folderContainsFolder(s.Deps.DB, folderID, searchingFolderID)
}

func folderContainsFolder(db *gorm.DB, folderID uint, searchingFolderID uint) (bool, error) {
	// A folder contains the target only if it is one of the target's ancestors.
	// Track visited parents so malformed existing cycles cannot loop forever.
	visited := make(map[uint]bool)
	for current := searchingFolderID; current > 0; {
		if visited[current] {
			return false, errors.New("folder hierarchy contains a cycle")
		}
		visited[current] = true

		var folder models.Folder
		if err := db.Select("id", "parent_folder_id").First(&folder, current).Error; err != nil {
			return false, err
		}
		if folder.ParentFolderID == folderID {
			return true, nil
		}
		current = folder.ParentFolderID
	}
	return false, nil
}
