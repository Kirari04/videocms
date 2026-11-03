package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"

	"gorm.io/gorm"
)

// ValidateFolderDestination enforces the ownership boundary for new children.
// Callers that insert records must repeat this check in their insert transaction.
func ValidateFolderDestination(db *gorm.DB, parentFolderID, userID uint) (int, error) {
	if parentFolderID == 0 {
		return http.StatusOK, nil
	}
	var folder models.Folder
	err := db.Where("id = ? AND user_id = ?", parentFolderID, userID).First(&folder).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return http.StatusBadRequest, errors.New("parent folder doesn't exist")
	}
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, nil
}
