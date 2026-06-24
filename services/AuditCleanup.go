package services

import (
	"ch/kirari04/videocms/models"
	"context"
	"log"
	"time"
)

func (w *WorkerGroup) AuditCleanup(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		w.runAuditCleanup()
		if !sleepContext(ctx, time.Hour) {
			return
		}
	}
}

/*
This function deletes audit logs older than 30 days
*/
func (w *WorkerGroup) runAuditCleanup() {
	expiryDate := time.Now().AddDate(0, 0, -30)

	result := w.deps.DB.Unscoped().Where("created_at < ?", expiryDate).Delete(&models.ApiKeyAuditLog{})
	if result.Error != nil {
		log.Printf("Failed to cleanup old audit logs: %v", result.Error)
		return
	}

	if result.RowsAffected > 0 {
		log.Printf("Cleaned up %d old API Key audit logs.", result.RowsAffected)
	}
}
