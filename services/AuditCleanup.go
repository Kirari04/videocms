package services

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"time"
)

func AuditCleanup() {
	for {
		runAuditCleanup()
		time.Sleep(time.Hour)
	}
}

/*
This function deletes audit logs older than 30 days
*/
func runAuditCleanup() {
	expiryDate := time.Now().AddDate(0, 0, -30)
	
	result := inits.DB.Unscoped().Where("created_at < ?", expiryDate).Delete(&models.ApiKeyAuditLog{})
	if result.Error != nil {
		log.Printf("Failed to cleanup old audit logs: %v", result.Error)
		return
	}

	if result.RowsAffected > 0 {
		log.Printf("Cleaned up %d old API Key audit logs.", result.RowsAffected)
	}
}
