package logic

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserOperationsRespectRequestGate(t *testing.T) {
	const userID uint = 42
	tests := []struct {
		name          string
		invoke        func(*Service) (int, error)
		validationErr string
	}{
		{
			name: "DeleteFolders",
			invoke: func(service *Service) (int, error) {
				return service.DeleteFolders(&models.FoldersDeleteValidation{}, userID, false)
			},
			validationErr: "array FolderIDs is empty",
		},
		{
			name: "SimpleUpload",
			invoke: func(service *Service) (int, error) {
				status, _, err := service.SimpleUpload(1, "file", strings.NewReader(""), 0, userID)
				return status, err
			},
			validationErr: "parent folder doesn't exist",
		},
		{
			name: "StageSimpleUpload",
			invoke: func(service *Service) (int, error) {
				status, _, err := service.StageSimpleUpload(1, "file", strings.NewReader(""), 0, userID)
				return status, err
			},
			validationErr: "parent folder doesn't exist",
		},
		{
			name: "StageSimpleUploadWithID",
			invoke: func(service *Service) (int, error) {
				status, _, err := service.StageSimpleUploadWithID(1, "file", strings.NewReader(""), 0, userID, "upload-id")
				return status, err
			},
			validationErr: "parent folder doesn't exist",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gate := app.NewRequestGate()
			service := NewService(&app.Deps{RequestGate: gate})
			if !gate.TryStart(userID) {
				t.Fatal("failed to acquire the gate for the active operation")
			}
			status, err := test.invoke(service)
			if status != http.StatusTooManyRequests || err == nil || err.Error() != "wait until the previous delete request finished" {
				t.Fatalf("blocked operation = (%d, %v), want existing 429 response", status, err)
			}
			if !gate.Blocked(userID) {
				t.Fatal("rejected operation released the active operation's gate")
			}
			gate.End(userID)

			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gate.db")), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.AutoMigrate(&models.Folder{}); err != nil {
				t.Fatal(err)
			}
			service.Deps.DB = db
			status, err = test.invoke(service)
			if status != http.StatusBadRequest || err == nil || err.Error() != test.validationErr {
				t.Fatalf("invalid operation = (%d, %v), want (400, %q)", status, err, test.validationErr)
			}
			if !gate.TryStart(userID) {
				t.Fatal("operation did not release the gate after returning a validation error")
			}
			gate.End(userID)
		})
	}
}
