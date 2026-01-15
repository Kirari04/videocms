package logic

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"
)

func MoveItems(userId uint, targetFolderId uint, folderIds []uint, linkIds []uint, isAdmin bool) (int, error) {
	// check if at least one item is being moved
	if len(folderIds) == 0 && len(linkIds) == 0 {
		return http.StatusBadRequest, errors.New("no items selected to move")
	}

	// 1. Validate Target Folder
	if targetFolderId > 0 {
		var targetFolder models.Folder
		if res := inits.DB.First(&targetFolder, targetFolderId); res.Error != nil {
			return http.StatusBadRequest, errors.New("target folder doesn't exist")
		}
		if !isAdmin && targetFolder.UserID != userId {
			return http.StatusForbidden, errors.New("unauthorized access to target folder")
		}
	}

	// 2. Move Folders
	for _, folderId := range folderIds {
		// Cannot move a folder into itself
		if folderId == targetFolderId {
			return http.StatusBadRequest, errors.New("cannot move folder into itself")
		}

		var folder models.Folder
		if res := inits.DB.First(&folder, folderId); res.Error != nil {
			return http.StatusBadRequest, errors.New("folder to move not found")
		}

		if !isAdmin && folder.UserID != userId {
			return http.StatusForbidden, errors.New("unauthorized access to folder")
		}

		// Loop detection: Check if the folder we are moving contains the target folder
		if targetFolderId > 0 {
			contains, err := helpers.FolderContainsFolder(folderId, targetFolderId)
			if err != nil {
				return http.StatusInternalServerError, err
			}
			if contains {
				return http.StatusBadRequest, errors.New("cannot move parent folder into its child")
			}
		}

		folder.ParentFolderID = targetFolderId
		if res := inits.DB.Save(&folder); res.Error != nil {
			return http.StatusInternalServerError, res.Error
		}
	}

	// 3. Move Links (Files)
	for _, linkId := range linkIds {
		var link models.Link
		if res := inits.DB.First(&link, linkId); res.Error != nil {
			return http.StatusBadRequest, errors.New("file to move not found")
		}

		if !isAdmin && link.UserID != userId {
			return http.StatusForbidden, errors.New("unauthorized access to file")
		}

		link.ParentFolderID = targetFolderId
		if res := inits.DB.Save(&link); res.Error != nil {
			return http.StatusInternalServerError, res.Error
		}
	}

	return http.StatusOK, nil
}
