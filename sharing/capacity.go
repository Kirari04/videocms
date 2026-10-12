package sharing

import (
	"time"

	"ch/kirari04/videocms/models"
	"gorm.io/gorm"
)

type Capacity struct {
	Used      int64
	Reserved  int64
	Remaining int64
}

// UploadCapacity counts source video bytes throughout the live subtree. Each
// pending upload group reserves its full declared size once, including all of
// its partial resources. Imported groups stop reserving once their link exists.
func UploadCapacity(db *gorm.DB, share *models.FolderShare, excludeGroup string) (Capacity, error) {
	var result Capacity
	err := db.Raw(`WITH RECURSIVE subtree(id) AS (
        SELECT id FROM folders WHERE id = ? AND user_id = ? AND deleted_at IS NULL
        UNION
        SELECT f.id FROM folders f JOIN subtree p ON f.parent_folder_id = p.id
        WHERE f.user_id = ? AND f.deleted_at IS NULL
    ), pending AS (
        SELECT u.client_upload_uuid, MAX(u.expected_size) AS expected, SUM(u.quota_bytes) AS reserved
        FROM upload_sessions u WHERE u.user_id = ? AND u.parent_folder_id IN (SELECT id FROM subtree)
        AND u.deleted_at IS NULL AND u.status IN ('created', 'uploading', 'uploaded', 'importing', 'failed')
        AND (u.expires_at IS NULL OR u.expires_at > ?) AND u.client_upload_uuid != ?
        AND NOT EXISTS (SELECT 1 FROM links l WHERE l.user_id = u.user_id AND l.deleted_at IS NULL
            AND l.creation_key = 'upload:' || CAST(u.user_id AS TEXT) || ':' || u.client_upload_uuid)
        GROUP BY u.client_upload_uuid
    ) SELECT
        COALESCE((SELECT SUM(f.size) FROM links l JOIN files f ON f.id = l.file_id
            WHERE l.user_id = ? AND l.parent_folder_id IN (SELECT id FROM subtree)
            AND l.deleted_at IS NULL AND f.deleted_at IS NULL), 0) AS used,
        COALESCE((SELECT SUM(CASE WHEN expected > reserved THEN expected ELSE reserved END) FROM pending), 0) AS reserved`,
		share.FolderID, share.UserID, share.UserID, share.UserID, time.Now(), excludeGroup, share.UserID).Scan(&result).Error
	result.Remaining = max(0, share.MaxFolderSize-result.Used-result.Reserved)
	return result, err
}
