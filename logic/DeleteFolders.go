package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
)

/*
This function shouldnt be runned in concurrency.
The reason for that is, if the user woulf start the process of delete for the root folder
and shortly after calls the delete method for the root folder too, it would try to list the
whole folder tree and delete all folders & files again. To prevent that there is an map variable
that should prevent the user from calling this method multiple times concurrently.
*/
func (s *Service) DeleteFolders(folderValidation *models.FoldersDeleteValidation, userID uint, isAdmin bool) (status int, err error) {

	if s.Deps.RequestGate.Blocked(userID) {
		return http.StatusTooManyRequests, errors.New("wait until the previous delete request finished")
	}
	s.Deps.RequestGate.Start(userID)
	defer s.Deps.RequestGate.End(userID)

	if len(folderValidation.FolderIDs) == 0 {
		return http.StatusBadRequest, errors.New("array FolderIDs is empty")
	}

	if int64(len(folderValidation.FolderIDs)) > s.Config().MaxItemsMultiDelete {
		return http.StatusBadRequest, errors.New("max requested items exceeded")
	}

	//check if requested folders exists
	reqFolderIdDeleteMap := make(map[uint]bool, len(folderValidation.FolderIDs))
	reqFolderIdDeleteList := []uint{}
	var parentFolderID uint = 0
	for i, FolderValidation := range folderValidation.FolderIDs {
		query := s.Deps.DB.Model(&models.Folder{})
		if !isAdmin {
			query = query.Where("user_id = ?", userID)
		}

		var dbFolder models.Folder
		if res := query.First(&dbFolder, FolderValidation.FolderID); res.Error != nil {
			return http.StatusBadRequest, fmt.Errorf("FolderID (%d) doesn't exist", FolderValidation.FolderID)
		}
		// check if has same parent folder
		if i == 0 {
			parentFolderID = dbFolder.ParentFolderID
		}
		if i > 0 {
			if parentFolderID != dbFolder.ParentFolderID {
				return http.StatusBadRequest, fmt.Errorf(
					"all folders have to share the same parent folder. Folder %d doesnt: %d (required) vs %d (actual)",
					FolderValidation.FolderID,
					parentFolderID,
					dbFolder.ParentFolderID,
				)
			}
		}

		if reqFolderIdDeleteMap[FolderValidation.FolderID] {
			return http.StatusBadRequest, fmt.Errorf(
				"the folders have to be distinct. Folder %d is dublicate",
				FolderValidation.FolderID,
			)
		}
		// add folder to todo list
		reqFolderIdDeleteList = append(reqFolderIdDeleteList, FolderValidation.FolderID)
		reqFolderIdDeleteMap[FolderValidation.FolderID] = true
	}

	// query all containing
	folders := []uint{}
	files := []models.LinkDeleteValidation{}

	for _, reqFolderId := range reqFolderIdDeleteList {
		s.listFolders(reqFolderId, &folders, &files)
		folders = append(folders, reqFolderId)
		s.listFiles(reqFolderId, &files)
	}

	if len(files) > 0 {
		if status, err := s.DeleteFiles(&models.LinksDeleteValidation{
			LinkIDs: files,
		}, userID, isAdmin); err != nil {
			return status, fmt.Errorf("failed to delete all files from folders: %v", err)
		}
	}

	if res := s.Deps.DB.Delete(&models.Folder{}, folders); res.Error != nil {
		return status, fmt.Errorf("failed to delete all folders: %v", err)
	}

	return http.StatusOK, nil
}

func (s *Service) listFolders(folderId uint, folders *[]uint, files *[]models.LinkDeleteValidation) {
	var folderList []models.Folder
	s.Deps.DB.Select("id").
		Where(&models.Folder{
			ParentFolderID: folderId,
		}).
		Find(&folderList)
	for _, id := range folderList {
		s.listFolders(id.ID, folders, files)
		*folders = append(*folders, id.ID)
		s.listFiles(id.ID, files)
	}
}

func (s *Service) listFiles(folderId uint, files *[]models.LinkDeleteValidation) {
	var fileList []models.Link
	s.Deps.DB.Select("id").
		Where(&models.Link{
			ParentFolderID: folderId,
		}).
		Find(&fileList)
	for _, id := range fileList {
		*files = append(*files, models.LinkDeleteValidation{
			LinkID: id.ID,
		})
	}
}
