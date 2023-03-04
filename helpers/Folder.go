package helpers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
)

func FolderContainsFolder(folderId uint, searchingFolderId uint) (bool, error) {
	// list all child folders of the current folder
	var children []models.Folder
	if res := inits.DB.Where(&models.Folder{
		ParentFolderID: folderId,
	}).Find(&children); res.Error != nil {
		return false, res.Error
	}

	// loop over the child folders
	for _, child := range children {
		// check if the current child folder matches the searching folder
		if child.ID == searchingFolderId {
			return true, nil
		}
		// check recursive if the child folders containing folders
		if contains, err := FolderContainsFolder(child.ID, searchingFolderId); err != nil || !contains {
			return contains, err
		}
	}

	// no search results
	return false, nil
}
