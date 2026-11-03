package sharing

import (
	"errors"
	"time"

	"ch/kirari04/videocms/models"
	"gorm.io/gorm"
)

var ErrUnavailable = errors.New("share is unavailable")
var ErrOutsideShare = errors.New("folder is outside this share")

// Resolve checks live state on every request, including removal of the owner or
// root folder. Sessions and media tokens never override a revoked share.
func Resolve(db *gorm.DB, token string) (*models.FolderShare, error) {
	var share models.FolderShare
	if len(token) != 43 {
		return nil, ErrUnavailable
	}
	if err := db.Where("token = ?", token).First(&share).Error; err != nil {
		return nil, err
	}
	if share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now()) {
		return nil, ErrUnavailable
	}
	var owner models.User
	if err := db.First(&owner, share.UserID).Error; err != nil {
		return nil, err
	}
	var folder models.Folder
	if err := db.Where("id = ? AND user_id = ?", share.FolderID, share.UserID).First(&folder).Error; err != nil {
		return nil, err
	}
	return &share, nil
}

// ContainsFolder walks current ancestry rather than caching membership, so a
// folder moved out of the shared subtree immediately loses shared access.
func ContainsFolder(db *gorm.DB, share *models.FolderShare, folderID uint) error {
	seen := make(map[uint]bool)
	for folderID != 0 && len(seen) < 256 {
		if seen[folderID] {
			return ErrOutsideShare
		}
		seen[folderID] = true
		var folder models.Folder
		if err := db.Where("id = ? AND user_id = ?", folderID, share.UserID).First(&folder).Error; err != nil {
			return ErrOutsideShare
		}
		if folder.ID == share.FolderID {
			return nil
		}
		folderID = folder.ParentFolderID
	}
	return ErrOutsideShare
}

func ContainsLink(db *gorm.DB, share *models.FolderShare, uuid string) error {
	var link models.Link
	if err := db.Where("uuid = ? AND user_id = ?", uuid, share.UserID).First(&link).Error; err != nil {
		return ErrOutsideShare
	}
	return ContainsFolder(db, share, link.ParentFolderID)
}
