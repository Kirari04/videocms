package logic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func deletionTestFolders(t *testing.T, s *Service, owner, parent uint, count int) []models.Folder {
	t.Helper()
	folders := make([]models.Folder, count)
	for i := range folders {
		folders[i] = models.Folder{UserID: owner, ParentFolderID: parent, Name: "folder"}
	}
	if err := s.Deps.DB.CreateInBatches(&folders, 100).Error; err != nil {
		t.Fatal(err)
	}
	return folders
}

func deletionTestLinks(t *testing.T, s *Service, owner, parent uint, count int) []models.Link {
	t.Helper()
	files := make([]models.File, count)
	for i := range files {
		files[i] = models.File{UUID: uuid.NewString(), StorageID: "local", UserID: owner}
	}
	if err := s.Deps.DB.CreateInBatches(&files, 100).Error; err != nil {
		t.Fatal(err)
	}
	links := make([]models.Link, count)
	for i := range links {
		links[i] = models.Link{UUID: uuid.NewString(), UserID: owner, ParentFolderID: parent, FileID: files[i].ID}
	}
	if err := s.Deps.DB.CreateInBatches(&links, 100).Error; err != nil {
		t.Fatal(err)
	}
	return links
}

func requireDeletionThumbnail(t *testing.T, s *Service, link models.Link, present bool) {
	t.Helper()
	key, err := s.Deps.Storage.Layout().Thumbnail(link.File.UUID, link.Thumbnail)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.Deps.Storage.Store("local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Stat(context.Background(), key)
	if present && err != nil {
		t.Fatalf("thumbnail was removed before commit: %v", err)
	}
	if !present && !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("thumbnail remains after deletion: %v", err)
	}
}

func TestFolderDeletionAllowsLargeTreeWithBoundedQueries(t *testing.T) {
	s, own, _ := ownershipTestService(t)
	root := ownershipFolder(t, s, own.ID, 0)
	children := deletionTestFolders(t, s, own.ID, root.ID, 2*deletionBatchSize+1)
	deletionTestLinks(t, s, own.ID, root.ID, 2*deletionBatchSize+1)
	poster := ownershipLink(t, s, own.ID, children[len(children)-1].ID)
	// A cycle in the final frontier batch must still visit each folder once.
	if err := s.Deps.DB.Model(&root).Update("parent_folder_id", children[len(children)-1].ID).Error; err != nil {
		t.Fatal(err)
	}

	frontierQueries := map[string]int{}
	deleteBatches := map[string]int{}
	if err := s.Deps.DB.Callback().Query().After("gorm:query").Register("test:bounded_deletion_queries", func(tx *gorm.DB) {
		if len(tx.Statement.Vars) > deletionBatchSize {
			tx.AddError(fmt.Errorf("query has %d parameters", len(tx.Statement.Vars)))
		}
		if strings.Contains(tx.Statement.SQL.String(), "parent_folder_id IN") {
			frontierQueries[tx.Statement.Table]++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Deps.DB.Callback().Delete().After("gorm:delete").Register("test:bounded_deletion_writes", func(tx *gorm.DB) {
		if len(tx.Statement.Vars) > deletionBatchSize+2 {
			tx.AddError(fmt.Errorf("delete has %d parameters", len(tx.Statement.Vars)))
		}
		deleteBatches[tx.Statement.Table]++
	}); err != nil {
		t.Fatal(err)
	}

	status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, false)
	if status != http.StatusOK || err != nil {
		t.Fatalf("large tree deletion = (%d, %v)", status, err)
	}
	for _, table := range []string{"folders", "links"} {
		if frontierQueries[table] != 4 {
			t.Errorf("%s frontier queries = %d, want 4", table, frontierQueries[table])
		}
		if deleteBatches[table] != 3 {
			t.Errorf("%s delete batches = %d, want 3", table, deleteBatches[table])
		}
	}
	requireOwnershipCounts(t, s, 0, 0)
	requireDeletionThumbnail(t, s, poster, false)
}

func TestDeletionRetainsSelectedItemLimits(t *testing.T) {
	for _, kind := range []string{"folders", "files"} {
		t.Run(kind, func(t *testing.T) {
			s, own, _ := ownershipTestService(t)
			count := int(s.Config().MaxItemsMultiDelete) + 1
			var status int
			var err error
			if kind == "folders" {
				folders := deletionTestFolders(t, s, own.ID, 0, count)
				poster := ownershipLink(t, s, own.ID, folders[0].ID)
				selection := &models.FoldersDeleteValidation{}
				for _, folder := range folders {
					selection.FolderIDs = append(selection.FolderIDs, models.FolderDeleteValidation{FolderID: folder.ID})
				}
				status, err = s.DeleteFolders(selection, own.ID, false)
				requireOwnershipCounts(t, s, int64(count), 1)
				requireDeletionThumbnail(t, s, poster, true)
			} else {
				links := deletionTestLinks(t, s, own.ID, 0, count)
				selection := &models.LinksDeleteValidation{}
				for _, link := range links {
					selection.LinkIDs = append(selection.LinkIDs, models.LinkDeleteValidation{LinkID: link.ID})
				}
				status, err = s.DeleteFiles(selection, own.ID, false)
				requireOwnershipCounts(t, s, 0, int64(count))
			}
			if status != http.StatusBadRequest || err == nil {
				t.Fatalf("oversized %s selection = (%d, %v)", kind, status, err)
			}
		})
	}
}

func TestFolderDeletionRollsBackEveryBatch(t *testing.T) {
	for _, table := range []string{"links", "folders"} {
		for _, failure := range []string{"write error", "changed owner"} {
			t.Run(table+" "+failure, func(t *testing.T) {
				s, own, foreign := ownershipTestService(t)
				root := ownershipFolder(t, s, own.ID, 0)
				children := deletionTestFolders(t, s, own.ID, root.ID, deletionBatchSize+1)
				deletionTestLinks(t, s, own.ID, root.ID, deletionBatchSize+1)
				posters := []models.Link{
					ownershipLink(t, s, own.ID, root.ID),
					ownershipLink(t, s, own.ID, children[len(children)-1].ID),
				}
				batches := 0
				if err := s.Deps.DB.Callback().Delete().Before("gorm:delete").Register("test:late_deletion_failure", func(tx *gorm.DB) {
					if tx.Statement.Table != table {
						return
					}
					batches++
					if batches != 2 {
						return
					}
					if failure == "write error" {
						tx.AddError(errors.New("injected late delete failure"))
						return
					}
					var model any = &models.Link{}
					if table == "folders" {
						model = &models.Folder{}
					}
					// Change all remaining live rows so the next batch's owner
					// predicate cannot match, regardless of collection order.
					result := tx.Session(&gorm.Session{NewDB: true}).Model(model).Where("user_id = ?", own.ID).Update("user_id", foreign.ID)
					if result.Error != nil {
						tx.AddError(result.Error)
					}
				}); err != nil {
					t.Fatal(err)
				}
				want := http.StatusInternalServerError
				if failure == "changed owner" {
					want = http.StatusForbidden
				}
				status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, false)
				if status != want || err == nil || batches != 2 {
					t.Fatalf("late failure = (%d, %v), batches = %d; want %d and 2", status, err, batches, want)
				}
				requireOwnershipCounts(t, s, int64(len(children)+1), deletionBatchSize+3)
				for _, model := range []any{&models.Link{}, &models.Folder{}} {
					var changed int64
					if err := s.Deps.DB.Model(model).Where("user_id = ?", foreign.ID).Count(&changed).Error; err != nil {
						t.Fatal(err)
					}
					if changed != 0 {
						t.Fatalf("%T ownership changes were not rolled back", model)
					}
				}
				for _, poster := range posters {
					requireDeletionThumbnail(t, s, poster, true)
				}
			})
		}
	}
}

func TestFolderDeletionRejectsForeignDescendantsBeyondFirstBatch(t *testing.T) {
	for _, kind := range []string{"folder", "link"} {
		t.Run(kind, func(t *testing.T) {
			s, own, foreign := ownershipTestService(t)
			root := ownershipFolder(t, s, own.ID, 0)
			children := deletionTestFolders(t, s, own.ID, root.ID, deletionBatchSize+1)
			deletionTestLinks(t, s, own.ID, root.ID, deletionBatchSize+1)
			poster := ownershipLink(t, s, own.ID, root.ID)
			last := children[len(children)-1]
			linkCount := int64(deletionBatchSize + 2)
			if kind == "folder" {
				ownershipFolder(t, s, foreign.ID, last.ID)
			} else {
				ownershipLink(t, s, foreign.ID, last.ID)
				linkCount++
			}
			status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, false)
			if status != http.StatusForbidden || err == nil {
				t.Fatalf("foreign descendant = (%d, %v)", status, err)
			}
			folderCount := int64(len(children) + 1)
			if kind == "folder" {
				folderCount++
			}
			requireOwnershipCounts(t, s, folderCount, linkCount)
			requireDeletionThumbnail(t, s, poster, true)
		})
	}
}
