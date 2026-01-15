package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

/*
This function shouldnt be runned in concurrency.
The reason for that is, if the user woulf start the process of delete for the root folder
and shortly after calls the delete method for the root folder too, it would try to list the
whole folder tree and delete all folders & files again. To prevent that there is an map variable
that should prevent the user from calling this method multiple times concurrently.
*/
func UpdateFolder(c echo.Context) error {
	// parse & validate request
	var folderValidation models.FolderUpdateValidation
	if status, err := helpers.Validate(c, &folderValidation); err != nil {
		return c.String(status, err.Error())
	}

	var dbFolder models.Folder
	//check if requested folder id exists
	if res := inits.DB.First(&dbFolder, folderValidation.FolderID); res.Error != nil {
		return c.String(http.StatusBadRequest, "Folder doesn't exist")
	}

	// Verify ownership
	userID := c.Get("UserID").(uint)
	isAdmin, _ := c.Get("Admin").(bool)
	if !isAdmin && dbFolder.UserID != userID {
		return c.String(http.StatusForbidden, "Unauthorized access to folder")
	}

	/*
		check if ParentfolderID aint root folder (=0)
		check if requested parent folder id exists
		TODO: also check if the new parent folder is not a child of current folder or the folder itself
	*/
	if folderValidation.ParentFolderID > 0 {
		var targetParent models.Folder
		if res := inits.DB.First(&targetParent, folderValidation.ParentFolderID); res.Error != nil {
			return c.String(http.StatusBadRequest, "Parent folder doesn't exist")
		}

		if !isAdmin && targetParent.UserID != userID {
			return c.String(http.StatusForbidden, "Unauthorized access to target parent folder")
		}

		// if the new parent folder is inside the current folder we return an
		// error so the folders wont be in an infinite loop
		containsFolder, err := helpers.FolderContainsFolder(dbFolder.ID, dbFolder.ParentFolderID)
		if err != nil {
			log.Printf("While running FolderContainsFolder the database returned an error: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
		if containsFolder {
			return c.String(http.StatusBadRequest, "Parent folder aint a parent folder in relation to new  exist")
		}
	}

	//update folder data
	dbFolder.Name = folderValidation.Name
	dbFolder.ParentFolderID = folderValidation.ParentFolderID
	if res := inits.DB.Save(&dbFolder); res.Error != nil {
		log.Printf("Failed to update folder: %v", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}
