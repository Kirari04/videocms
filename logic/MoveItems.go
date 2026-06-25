package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"
)

func (s *Service) MoveItems(userId uint, targetFolderId uint, folderIds []uint, linkIds []uint, isAdmin bool) (int, error) {
	// check if at least one item is being moved
	if len(folderIds) == 0 && len(linkIds) == 0 {
		return http.StatusBadRequest, errors.New("no items selected to move")
	}

	// 1. Validate Target Folder
	var targetFolderOwnerID uint
	if targetFolderId > 0 {
		var targetFolder models.Folder
		if res := s.Deps.DB.First(&targetFolder, targetFolderId); res.Error != nil {
			return http.StatusBadRequest, errors.New("target folder doesn't exist")
		}
		if !isAdmin && targetFolder.UserID != userId {
			return http.StatusForbidden, errors.New("unauthorized access to target folder")
		}
		targetFolderOwnerID = targetFolder.UserID
	}

	// 2. Move Folders
	for _, folderId := range folderIds {
		// Cannot move a folder into itself
		if folderId == targetFolderId {
			return http.StatusBadRequest, errors.New("cannot move folder into itself")
		}

		var folder models.Folder
		if res := s.Deps.DB.First(&folder, folderId); res.Error != nil {
			return http.StatusBadRequest, errors.New("folder to move not found")
		}

		if !isAdmin && folder.UserID != userId {
			return http.StatusForbidden, errors.New("unauthorized access to folder")
		}

		// Ensure target folder belongs to the same user as the item (even for admins)
		if targetFolderId > 0 && folder.UserID != targetFolderOwnerID {
			return http.StatusBadRequest, errors.New("cannot move folder to a destination owned by another user")
		}

		// Loop detection: Check if the folder we are moving contains the target folder
		if targetFolderId > 0 {
			contains, err := s.FolderContainsFolder(folderId, targetFolderId)
			if err != nil {
				return http.StatusInternalServerError, err
			}
			if contains {
				return http.StatusBadRequest, errors.New("cannot move parent folder into its child")
			}
		}

		folder.ParentFolderID = targetFolderId
		if res := s.Deps.DB.Save(&folder); res.Error != nil {
			return http.StatusInternalServerError, res.Error
		}
	}

	// 3. Move Links (Files)
	for _, linkId := range linkIds {
		var link models.Link
		if res := s.Deps.DB.First(&link, linkId); res.Error != nil {
			return http.StatusBadRequest, errors.New("file to move not found")
		}

		if !isAdmin && link.UserID != userId {
			return http.StatusForbidden, errors.New("unauthorized access to file")
		}

		// Ensure target folder belongs to the same user as the item (even for admins)
		if targetFolderId > 0 && link.UserID != targetFolderOwnerID {
			return http.StatusBadRequest, errors.New("cannot move file to a destination owned by another user")
		}

		link.ParentFolderID = targetFolderId
		if res := s.Deps.DB.Save(&link); res.Error != nil {
			return http.StatusInternalServerError, res.Error
		}
	}

	return http.StatusOK, nil
}

func (s *Service) FolderContainsFolder(folderID uint, searchingFolderID uint) (bool, error) {
	var children []models.Folder
	if res := s.Deps.DB.Where(&models.Folder{
		ParentFolderID: folderID,
	}).Find(&children); res.Error != nil {
		return false, res.Error
	}

	for _, child := range children {
		if child.ID == searchingFolderID {
			return true, nil
		}
		contains, err := s.FolderContainsFolder(child.ID, searchingFolderID)
		if err != nil || contains {
			return contains, err
		}
	}

	return false, nil
}
