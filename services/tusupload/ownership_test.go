package tusupload

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"ch/kirari04/videocms/models"
	tusd "github.com/tus/tusd/v2/pkg/handler"
	"gorm.io/gorm"
)

func TestTusOwnershipCreateDestination(t *testing.T) {
	for _, state := range []string{"root", "own parent", "foreign parent", "admin foreign parent", "deleted parent", "query error"} {
		t.Run(state, func(t *testing.T) {
			env := setupTusTest(t)
			owner := env.user.ID
			if state == "foreign parent" || state == "admin foreign parent" {
				owner++
			}
			parent := models.Folder{Name: "parent", UserID: owner}
			if err := env.db.Create(&parent).Error; err != nil {
				t.Fatal(err)
			}
			parentID := parent.ID
			if state == "root" {
				parentID = 0
			}
			if state == "deleted parent" {
				if err := env.db.Delete(&parent).Error; err != nil {
					t.Fatal(err)
				}
			}
			if state == "admin foreign parent" {
				env.user.Admin = true
				if err := env.db.Model(&env.user).Update("admin", true).Error; err != nil {
					t.Fatal(err)
				}
				env.token = testJWT(t, env.user, env.config().JwtSecretKey)
			}
			if state == "query error" {
				if err := env.db.Callback().Query().Before("gorm:query").Register("test:parent_error", func(tx *gorm.DB) {
					if tx.Statement.Table == "folders" {
						tx.AddError(errors.New("injected parent query error"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			rec := createTusUploadRequest(t, env, 10, map[string]string{"filename": "movie.mp4", "parent_folder_id": strconv.FormatUint(uint64(parentID), 10)})
			want := http.StatusBadRequest
			if state == "root" || state == "own parent" {
				want = http.StatusCreated
			}
			if state == "query error" {
				want = http.StatusInternalServerError
			}
			if rec.Code != want {
				t.Fatalf("create = %d: %s, want %d", rec.Code, rec.Body.String(), want)
			}
			if want != http.StatusCreated {
				var sessions int64
				if err := env.db.Model(&models.UploadSession{}).Count(&sessions).Error; err != nil {
					t.Fatal(err)
				}
				if sessions != 0 {
					t.Fatalf("rejected request created %d sessions", sessions)
				}
			}
		})
	}
}

func TestTusOwnershipCreateRevalidatesParentAtInsertion(t *testing.T) {
	env := setupTusTest(t)
	parent := models.Folder{Name: "parent", UserID: env.user.ID}
	if err := env.db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	deleted := false
	if err := env.db.Callback().Query().After("gorm:query").Register("test:delete_parent", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && !deleted {
			deleted = true
			if err := tx.Session(&gorm.Session{NewDB: true}).Delete(&parent).Error; err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := env.svc.preUploadCreate(tusd.HookEvent{Context: context.WithValue(context.Background(), userIDContextKey, env.user.ID), Upload: tusd.FileInfo{Size: 10, MetaData: tusd.MetaData{"filename": "movie.mp4", "parent_folder_id": strconv.FormatUint(uint64(parent.ID), 10)}}})
	if !deleted || err == nil {
		t.Fatalf("create error = %v, parent deleted=%v", err, deleted)
	}
	var tusErr tusd.Error
	if !errors.As(err, &tusErr) || tusErr.HTTPResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("create error = %#v, want 400", err)
	}
	var sessions int64
	if err := env.db.Model(&models.UploadSession{}).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("created %d orphan sessions", sessions)
	}
}
