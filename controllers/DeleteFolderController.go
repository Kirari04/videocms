package controllers

import (
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
func DeleteFolder(c *fiber.Ctx) error {
	userID := c.Locals("UserID").(uint)
	if helpers.UserRequestAsyncObj.Blocked(userID) {
		return c.Status(fiber.StatusTooManyRequests).SendString("Wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userID)
	defer helpers.UserRequestAsyncObj.End(userID)

	// parse & validate request
	var folderValidation models.FolderDeleteValidation
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

	//check if requested folder exists
	if res := inits.DB.First(&models.Folder{}, folderValidation.FolderID); res.Error != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FolderID",
				Tag:         "exists",
				Value:       "Parent folder doesn't exist",
			},
		})
	}

	// query all containing
	folderIdDeleteList := []uint{}
	linkIdDeleteList := []uint{}
	if err := deleteFolderRecursive(&folderIdDeleteList, &linkIdDeleteList, &folderValidation.FolderID, &userID); err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// add top if is not 0
	if folderValidation.FolderID > 0 {
		folderIdDeleteList = append(folderIdDeleteList, folderValidation.FolderID)
		inits.DB.Delete(&models.Folder{}, folderValidation.FolderID)
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
