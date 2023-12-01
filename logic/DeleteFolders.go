package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

/*
This function shouldnt be runned in concurrency.
The reason for that is, if the user woulf start the process of delete for the root folder
and shortly after calls the delete method for the root folder too, it would try to list the
whole folder tree and delete all folders & files again. To prevent that there is an map variable
that should prevent the user from calling this method multiple times concurrently.
*/
func DeleteFolders(folderValidation *models.FoldersDeleteValidation, userID uint) (status int, err error) {

	if helpers.UserRequestAsyncObj.Blocked(userID) {
		return fiber.StatusTooManyRequests, errors.New("wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userID)
	defer helpers.UserRequestAsyncObj.End(userID)

	if len(folderValidation.FolderIDs) == 0 {
		return fiber.StatusBadRequest, errors.New("array FolderIDs is empty")
	}

	if int64(len(folderValidation.FolderIDs)) > config.ENV.MaxItemsMultiDelete {
		return fiber.StatusBadRequest, errors.New("max requested items exceeded")
	}

	//check if requested folders exists
	reqFolderIdDeleteMap := make(map[uint]bool, len(folderValidation.FolderIDs))
	reqFolderIdDeleteList := []uint{}
	var parentFolderID uint = 0
	for i, FolderValidation := range folderValidation.FolderIDs {
		var dbFolder = models.Folder{
			UserID: userID,
		}
		if res := inits.DB.First(&dbFolder, FolderValidation.FolderID); res.Error != nil {
			return fiber.StatusBadRequest, fmt.Errorf("FolderID (%d) doesn't exist", FolderValidation.FolderID)
		}
		// check if has same parent folder
		if i == 0 {
			parentFolderID = dbFolder.ParentFolderID
		}
		if i > 0 {
			if parentFolderID != dbFolder.ParentFolderID {
				return fiber.StatusBadRequest, fmt.Errorf(
					"all folders have to share the same parent folder. Folder %d doesnt: %d (required) vs %d (actual)",
					FolderValidation.FolderID,
					parentFolderID,
					dbFolder.ParentFolderID,
				)
			}
		}

		if reqFolderIdDeleteMap[FolderValidation.FolderID] {
			return fiber.StatusBadRequest, fmt.Errorf(
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
		listFolders(reqFolderId, &folders, &files)
		folders = append(folders, reqFolderId)
		listFiles(reqFolderId, &files)
	}

	if len(files) > 0 {
		if status, err := DeleteFiles(&models.LinksDeleteValidation{
			LinkIDs: files,
		}, userID); err != nil {
			return status, fmt.Errorf("failed to delete all files from folders: %v", err)
		}
	}

	if res := inits.DB.Delete(&models.Folder{}, folders); res.Error != nil {
		return status, fmt.Errorf("failed to delete all folders: %v", err)
	}

	return fiber.StatusOK, nil
}

func listFolders(folderId uint, folders *[]uint, files *[]models.LinkDeleteValidation) {
	var folderList []models.Folder
	inits.DB.Select("id").
		Where(&models.Folder{
			ParentFolderID: folderId,
		}).
		Find(&folderList)
	for _, id := range folderList {
		listFolders(id.ID, folders, files)
		*folders = append(*folders, id.ID)
		listFiles(id.ID, files)
	}
}

func listFiles(folderId uint, files *[]models.LinkDeleteValidation) {
	var fileList []models.Link
	inits.DB.Select("id").
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
