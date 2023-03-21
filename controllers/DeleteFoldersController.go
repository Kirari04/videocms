package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
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
func DeleteFolders(c *fiber.Ctx) error {
	userID := c.Locals("UserID").(uint)
	if helpers.UserRequestAsyncObj.Blocked(userID) {
		return c.Status(fiber.StatusTooManyRequests).SendString("Wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userID)
	defer helpers.UserRequestAsyncObj.End(userID)

	// parse & validate request
	var folderValidation models.FoldersDeleteValidation
	if err := c.BodyParser(&folderValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}
	if errors := helpers.ValidateStruct(folderValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	if len(folderValidation.FolderIDs) == 0 {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FolderIDs",
				Tag:         "none",
				Value:       "Array is empty",
			},
		})
	}

	if int64(len(folderValidation.FolderIDs)) > config.ENV.MaxItemsMultiDelete {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FolderIDs",
				Tag:         "none",
				Value:       "Max requested items exceeded",
			},
		})
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
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "FolderID",
					Tag:         "exists",
					Value:       fmt.Sprintf("FolderID (%d) doesn't exist", FolderValidation.FolderID),
				},
			})
		}
		// check if has same parent folder
		if i == 0 {
			parentFolderID = dbFolder.ParentFolderID
		}
		if i > 0 {
			if parentFolderID != dbFolder.ParentFolderID {
				return c.Status(400).JSON([]helpers.ValidationError{
					{
						FailedField: "none",
						Tag:         "none",
						Value: fmt.Sprintf(
							"All folders have to share the same parent folder. Folder %d doesnt: %d (required) vs %d (actual)",
							FolderValidation.FolderID,
							parentFolderID,
							dbFolder.ParentFolderID,
						),
					},
				})
			}
		}

		if reqFolderIdDeleteMap[FolderValidation.FolderID] {
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "FolderID",
					Tag:         "distinct",
					Value: fmt.Sprintf(
						"The folders have to be distinct. Folder %d is dublicate",
						FolderValidation.FolderID,
					),
				},
			})
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
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		// add top if is not 0
		if reqFolderId > 0 {
			folderIdDeleteList = append(folderIdDeleteList, reqFolderId)
			inits.DB.Delete(&models.Folder{}, reqFolderId)
		}
	}

	// delete first files so folder structure stays if it fails
	if len(linkIdDeleteList) > 0 {
		if res := inits.DB.Delete(&models.Link{}, linkIdDeleteList); res.Error != nil {
			log.Printf("Failed to delete files from id list: %v", res.Error)
			log.Println(linkIdDeleteList)
			return c.SendStatus(fiber.StatusInternalServerError)
		}
	}

	//delete folders
	if res := inits.DB.Delete(&models.Folder{}, folderIdDeleteList); res.Error != nil {
		log.Printf("Failed to delete folders from id list: %v", res.Error)
		log.Println(folderIdDeleteList)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(200)
}
