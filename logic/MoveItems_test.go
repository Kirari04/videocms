package logic

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"

	"gorm.io/gorm"
)

func newMoveItemsTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := inits.OpenDatabase(filepath.Join(t.TempDir(), "moves.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Folder{}, &models.Link{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return NewService(&app.Deps{DB: db.WithContext(ctx)}), db
}

func moveTestFolder(t *testing.T, db *gorm.DB, owner, parent uint, name string) models.Folder {
	t.Helper()
	folder := models.Folder{Name: name, UserID: owner, ParentFolderID: parent}
	if err := db.Create(&folder).Error; err != nil {
		t.Fatal(err)
	}
	return folder
}

func moveTestLink(t *testing.T, db *gorm.DB, owner, parent uint, name string) models.Link {
	t.Helper()
	link := models.Link{Name: name, UUID: name, UserID: owner, ParentFolderID: parent, FileID: 42, Thumbnail: "thumbnail"}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	return link
}

func assertMoveParents(t *testing.T, db *gorm.DB, folders []models.Folder, links []models.Link, parent uint) {
	t.Helper()
	for _, original := range folders {
		var folder models.Folder
		if err := db.First(&folder, original.ID).Error; err != nil {
			t.Fatal(err)
		}
		if folder.ParentFolderID != parent || folder.UserID != original.UserID {
			t.Fatalf("folder %d = parent %d, owner %d; want parent %d, owner %d", folder.ID, folder.ParentFolderID, folder.UserID, parent, original.UserID)
		}
	}
	for _, original := range links {
		var link models.Link
		if err := db.First(&link, original.ID).Error; err != nil {
			t.Fatal(err)
		}
		if link.ParentFolderID != parent || link.UserID != original.UserID || link.FileID != original.FileID || link.UUID != original.UUID {
			t.Fatalf("link %d changed unexpected fields: %#v", link.ID, link)
		}
	}
}

func TestMoveItemsValidatesEntireSelectionBeforeUpdates(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
	}{
		{"missing folder", http.StatusBadRequest},
		{"forbidden folder", http.StatusForbidden},
		{"missing link", http.StatusBadRequest},
		{"forbidden link", http.StatusForbidden},
		{"self folder", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db := newMoveItemsTestService(t)
			origin := moveTestFolder(t, db, 1, 0, "origin")
			target := moveTestFolder(t, db, 1, 0, "target")
			folder := moveTestFolder(t, db, 1, origin.ID, "folder")
			link := moveTestLink(t, db, 1, origin.ID, "link")
			folderIDs, linkIDs := []uint{folder.ID}, []uint{link.ID}
			switch tc.name {
			case "missing folder":
				folderIDs = append(folderIDs, 99999)
			case "forbidden folder":
				other := moveTestFolder(t, db, 2, 0, "other folder")
				folderIDs = append(folderIDs, other.ID)
			case "missing link":
				linkIDs = append(linkIDs, 99999)
			case "forbidden link":
				other := moveTestLink(t, db, 2, 0, "other link")
				linkIDs = append(linkIDs, other.ID)
			case "self folder":
				folderIDs = append(folderIDs, target.ID)
			}
			updates := 0
			if err := db.Callback().Update().Before("gorm:update").Register("test:count-move-updates", func(tx *gorm.DB) {
				updates++
			}); err != nil {
				t.Fatal(err)
			}

			status, err := service.MoveItems(1, target.ID, folderIDs, linkIDs, false)
			if err == nil || status != tc.want {
				t.Fatalf("move = status %d, error %v; want %d", status, err, tc.want)
			}
			if updates != 0 {
				t.Fatalf("invalid selection attempted %d updates", updates)
			}
			assertMoveParents(t, db, []models.Folder{folder}, []models.Link{link}, origin.ID)
		})
	}
}

func TestMoveItemsRollsBackEarlierUpdatesOnWriteFailure(t *testing.T) {
	for _, table := range []string{"folders", "links"} {
		t.Run(table, func(t *testing.T) {
			service, db := newMoveItemsTestService(t)
			origin := moveTestFolder(t, db, 1, 0, "origin")
			target := moveTestFolder(t, db, 1, 0, "target")
			folders := []models.Folder{
				moveTestFolder(t, db, 1, origin.ID, "first folder"),
				moveTestFolder(t, db, 1, origin.ID, "second folder"),
			}
			links := []models.Link{
				moveTestLink(t, db, 1, origin.ID, "first link"),
				moveTestLink(t, db, 1, origin.ID, "second link"),
			}
			failureID, wantWrites := folders[1].ID, 1
			if table == "links" {
				failureID, wantWrites = links[1].ID, 3
			}
			trigger := fmt.Sprintf("CREATE TRIGGER fail_move BEFORE UPDATE OF parent_folder_id ON %s WHEN NEW.id = %d BEGIN SELECT RAISE(ABORT, 'forced move failure'); END", table, failureID)
			if err := db.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			writes := 0
			if err := db.Callback().Update().After("gorm:update").Register("test:count-successful-move-writes", func(tx *gorm.DB) {
				if tx.Error == nil {
					writes++
				}
			}); err != nil {
				t.Fatal(err)
			}

			status, err := service.MoveItems(1, target.ID, []uint{folders[0].ID, folders[1].ID}, []uint{links[0].ID, links[1].ID}, false)
			if status != http.StatusInternalServerError || err == nil || !strings.Contains(err.Error(), "forced move failure") {
				t.Fatalf("move = status %d, error %v", status, err)
			}
			if writes != wantWrites {
				t.Fatalf("successful writes before failure = %d, want %d", writes, wantWrites)
			}
			assertMoveParents(t, db, folders, links, origin.ID)
		})
	}
}

func TestMoveItemsPreservesRootAndAdminOwnershipPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		root, admin            bool
		folderOwner, linkOwner uint
		targetOwner            uint
		want                   int
	}{
		{"mixed selection", false, false, 1, 1, 1, http.StatusOK},
		{"root", true, false, 1, 1, 1, http.StatusOK},
		{"admin same owner", false, true, 2, 2, 2, http.StatusOK},
		{"admin folder owner mismatch", false, true, 2, 1, 1, http.StatusBadRequest},
		{"admin link owner mismatch", false, true, 1, 2, 1, http.StatusBadRequest},
		{"admin root mixed owners", true, true, 2, 3, 1, http.StatusOK},
		{"foreign target", false, false, 1, 1, 2, http.StatusForbidden},
		{"foreign root item", true, false, 2, 2, 1, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db := newMoveItemsTestService(t)
			origin := moveTestFolder(t, db, tc.folderOwner, 0, "origin")
			target := moveTestFolder(t, db, tc.targetOwner, 0, "target")
			folder := moveTestFolder(t, db, tc.folderOwner, origin.ID, "folder")
			link := moveTestLink(t, db, tc.linkOwner, origin.ID, "link")
			targetID := target.ID
			if tc.root {
				targetID = 0
			}
			status, err := service.MoveItems(1, targetID, []uint{folder.ID}, []uint{link.ID}, tc.admin)
			if status != tc.want || (err != nil) != (tc.want != http.StatusOK) {
				t.Fatalf("move = status %d, error %v; want %d", status, err, tc.want)
			}
			parent := targetID
			if err != nil {
				parent = origin.ID
			}
			assertMoveParents(t, db, []models.Folder{folder}, []models.Link{link}, parent)
		})
	}
}

func TestMoveItemsUpdatesOnlyParentFields(t *testing.T) {
	service, db := newMoveItemsTestService(t)
	folder := moveTestFolder(t, db, 1, 0, "folder")
	link := moveTestLink(t, db, 1, 0, "link")
	target := moveTestFolder(t, db, 1, 0, "target")
	if err := db.Callback().Update().Before("gorm:update").Register("test:edit-name-before-move", func(tx *gorm.DB) {
		switch tx.Statement.Table {
		case "folders":
			if err := tx.Exec("UPDATE folders SET name = ? WHERE id = ?", "edited folder", folder.ID).Error; err != nil {
				tx.AddError(err)
			}
		case "links":
			if err := tx.Exec("UPDATE links SET name = ? WHERE id = ?", "edited link", link.ID).Error; err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	status, err := service.MoveItems(1, target.ID, []uint{folder.ID}, []uint{link.ID}, false)
	if err != nil || status != http.StatusOK {
		t.Fatalf("move = status %d, error %v", status, err)
	}
	assertMoveParents(t, db, []models.Folder{folder}, []models.Link{link}, target.ID)
	if err := db.First(&folder, folder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&link, link.ID).Error; err != nil {
		t.Fatal(err)
	}
	if folder.Name != "edited folder" || link.Name != "edited link" {
		t.Fatalf("move overwrote another edit: folder %q, link %q", folder.Name, link.Name)
	}
}

func TestMoveItemsRejectsDescendantDestination(t *testing.T) {
	service, db := newMoveItemsTestService(t)
	parent := moveTestFolder(t, db, 1, 0, "parent")
	child := moveTestFolder(t, db, 1, parent.ID, "child")
	grandchild := moveTestFolder(t, db, 1, child.ID, "grandchild")
	link := moveTestLink(t, db, 1, 0, "link")
	status, err := service.MoveItems(1, grandchild.ID, []uint{parent.ID}, []uint{link.ID}, false)
	if err == nil || status != http.StatusBadRequest {
		t.Fatalf("move = status %d, error %v", status, err)
	}
	assertMoveParents(t, db, []models.Folder{parent}, []models.Link{link}, 0)
}

func TestMoveItemsStopsAtMalformedExistingCycle(t *testing.T) {
	service, db := newMoveItemsTestService(t)
	source := moveTestFolder(t, db, 1, 0, "source")
	first := moveTestFolder(t, db, 1, 0, "first")
	second := moveTestFolder(t, db, 1, first.ID, "second")
	if err := db.Model(&first).Update("parent_folder_id", second.ID).Error; err != nil {
		t.Fatal(err)
	}
	status, err := service.MoveItems(1, second.ID, []uint{source.ID}, nil, false)
	if err == nil || status != http.StatusInternalServerError || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("move = status %d, error %v", status, err)
	}
	assertMoveParents(t, db, []models.Folder{source}, nil, 0)
}

func TestMoveItemsConcurrentOpposingMovesCannotCreateCycle(t *testing.T) {
	service, db := newMoveItemsTestService(t)
	for i := 0; i < 10; i++ {
		first := moveTestFolder(t, db, 1, 0, fmt.Sprintf("first-%d", i))
		second := moveTestFolder(t, db, 1, 0, fmt.Sprintf("second-%d", i))
		start := make(chan struct{})
		results := make(chan int, 2)
		move := func(source, destination uint) {
			<-start
			status, _ := service.MoveItems(1, destination, []uint{source}, nil, false)
			results <- status
		}
		go move(first.ID, second.ID)
		go move(second.ID, first.ID)
		close(start)
		statuses := map[int]int{}
		statuses[<-results]++
		statuses[<-results]++
		if statuses[http.StatusOK] != 1 || statuses[http.StatusBadRequest] != 1 {
			t.Fatalf("opposing move results = %v", statuses)
		}
		if err := db.First(&first, first.ID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.First(&second, second.ID).Error; err != nil {
			t.Fatal(err)
		}
		if first.ParentFolderID == second.ID && second.ParentFolderID == first.ID {
			t.Fatal("opposing moves created a cycle")
		}
	}
}
