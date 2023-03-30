package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"

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
	folderIdDeleteList := []uint{}
	linkIdDeleteList := []uint{}

	for _, reqFolderId := range reqFolderIdDeleteList {
		if err := deleteFolderRecursive(&folderIdDeleteList, &linkIdDeleteList, &reqFolderId, &userID); err != nil {
			return fiber.StatusInternalServerError, errors.New("")
		}

		if err := deleteLinksFromFolder(&folderIdDeleteList, &linkIdDeleteList, &reqFolderId, &userID); err != nil {
			return fiber.StatusInternalServerError, errors.New("")
		}

		// add top if is not 0
		if reqFolderId > 0 {
			folderIdDeleteList = append(folderIdDeleteList, reqFolderId)
			inits.DB.Delete(&models.Folder{}, reqFolderId)
		}
	}

	// delete first files so folder structure stays if it fails
	if len(linkIdDeleteList) > 0 {
		deleteVasadeList := make([]models.LinkDeleteValidation, len(linkIdDeleteList))
		for i, linkIdDeleteItem := range linkIdDeleteList {
			deleteVasadeList[i] = models.LinkDeleteValidation{
				LinkID: linkIdDeleteItem,
			}
		}
		if status, err := DeleteFiles(&models.LinksDeleteValidation{
			LinkIDs: deleteVasadeList,
		}, userID); err != nil {
			return status, err
		}
	}

	//delete folders
	if res := inits.DB.Delete(&models.Folder{}, folderIdDeleteList); res.Error != nil {
		log.Printf("Failed to delete folders from id list: %v", res.Error)
		log.Println(folderIdDeleteList)
		return fiber.StatusInternalServerError, errors.New("")
	}

	return fiber.StatusOK, nil
}

func deleteFolderRecursive(folderIdDeleteList *[]uint, linkIdDeleteList *[]uint, ParentFolderIDptr *uint, UserIDptr *uint) error {
	var folders []models.Folder

	if res := inits.DB.
		Model(&models.Folder{}).
		Preload("User").
		Where(&models.Folder{
			ParentFolderID: *ParentFolderIDptr,
			UserID:         *UserIDptr,
		}, "ParentFolderID", "UserID").
		Find(&folders); res.Error != nil {
		return fmt.Errorf("failed to get a list of folders from parentfolder: %v", res.Error)
	}

	for _, folder := range folders {
		// delete first the containing folders of the current folder
		if err := deleteFolderRecursive(folderIdDeleteList, linkIdDeleteList, &folder.ID, UserIDptr); err != nil {
			return err
		}
		// delete all files in the current folder
		if err := deleteLinksFromFolder(folderIdDeleteList, linkIdDeleteList, &folder.ID, UserIDptr); err != nil {
			return err
		}
		// add current folder to list
		*folderIdDeleteList = append(*folderIdDeleteList, folder.ID)
	}

	return nil
}

func deleteLinksFromFolder(folderIdDeleteList *[]uint, linkIdDeleteList *[]uint, ParentFolderIDptr *uint, UserIDptr *uint) error {
	var links []models.Link
	res := inits.DB.
		Model(&models.Link{}).
		Where(&models.Link{
			UserID:         *UserIDptr,
			ParentFolderID: *ParentFolderIDptr,
		}).
		Find(&links)
	if res.Error != nil {
		return fmt.Errorf("failed to list the links from the parentfolder for deletion: %v", res.Error)
	}
	for _, v := range links {
		*linkIdDeleteList = append(*linkIdDeleteList, v.ID)
	}

	return nil
}
