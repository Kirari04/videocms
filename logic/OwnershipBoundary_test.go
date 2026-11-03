package logic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func ownershipTestService(t *testing.T) (*Service, models.User, models.User) {
	t.Helper()
	db := newStorageAdminTestDB(t)
	if err := db.AutoMigrate(&models.Folder{}, &models.UploadSession{}, &models.UploadLog{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	own := models.User{Username: "owner", Settings: models.UserSettings{UploadSessionsMax: 10}}
	foreign := models.User{Username: "foreign"}
	for _, user := range []*models.User{&own, &foreign} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	local, err := storage.NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	stores, err := storage.NewService("local", storage.LegacyMediaLayout{}, map[string]storage.Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	return NewService(&app.Deps{DB: db, Storage: stores, RequestGate: app.NewRequestGate(), Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{MaxItemsMultiDelete: 100, MaxUploadSessions: 10, FolderVideoUploadsPriv: root, FolderVideoQualitysPriv: root}})}), own, foreign
}

func ownershipFolder(t *testing.T, s *Service, owner, parent uint) models.Folder {
	t.Helper()
	f := models.Folder{UserID: owner, ParentFolderID: parent, Name: "folder"}
	if err := s.Deps.DB.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	return f
}
func ownershipLink(t *testing.T, s *Service, owner, parent uint) models.Link {
	t.Helper()
	f := models.File{UUID: uuid.NewString(), StorageID: "local", UserID: owner, Hash: "content", Size: 7}
	if err := s.Deps.DB.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	l := models.Link{UUID: uuid.NewString(), UserID: owner, ParentFolderID: parent, FileID: f.ID, Thumbnail: "custom.webp"}
	if err := s.Deps.DB.Create(&l).Error; err != nil {
		t.Fatal(err)
	}
	l.File = f
	key, err := s.Deps.Storage.Layout().Thumbnail(f.UUID, l.Thumbnail)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.Deps.Storage.Store("local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), key, strings.NewReader("poster"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	return l
}
func requireOwnershipCounts(t *testing.T, s *Service, folders, links int64) {
	t.Helper()
	var got int64
	if err := s.Deps.DB.Model(&models.Folder{}).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != folders {
		t.Fatalf("folders = %d, want %d", got, folders)
	}
	if err := s.Deps.DB.Model(&models.Link{}).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != links {
		t.Fatalf("links = %d, want %d", got, links)
	}
}

func TestOwnershipCreationRejectsForeignDestination(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, method := range []string{"folder", "clone", "file", "legacy upload", "staged upload"} {
			t.Run(method+map[bool]string{false: " user", true: " admin"}[admin], func(t *testing.T) {
				s, own, foreign := ownershipTestService(t)
				if err := s.Deps.DB.Model(&own).Update("admin", admin).Error; err != nil {
					t.Fatal(err)
				}
				parent := ownershipFolder(t, s, foreign.ID, 0)
				var status int
				var err error
				switch method {
				case "folder":
					status, _, err = s.CreateFolder("child", parent.ID, own.ID)
				case "clone":
					status, _, err = s.CloneFileByHash("content", parent.ID, "movie.mp4", own.ID, "")
				case "file":
					status, _, _, err = s.CreateFileContext(context.Background(), nil, parent.ID, "movie.mp4", "id", 7, own.ID, "")
				case "legacy upload":
					status, _, err = s.SimpleUpload(parent.ID, "movie.mp4", strings.NewReader("content"), 7, own.ID)
				case "staged upload":
					status, _, err = s.StageSimpleUpload(parent.ID, "movie.mp4", strings.NewReader("content"), 7, own.ID)
				}
				if status != http.StatusBadRequest || err == nil {
					t.Fatalf("creation = (%d,%v), want denied", status, err)
				}
				requireOwnershipCounts(t, s, 1, 0)
				var sessions int64
				if err := s.Deps.DB.Model(&models.UploadSession{}).Count(&sessions).Error; err != nil {
					t.Fatal(err)
				}
				if sessions != 0 {
					t.Fatalf("sessions = %d", sessions)
				}
			})
		}
	}
}

func TestOwnershipCreationAllowsRootAndOwnParent(t *testing.T) {
	s, own, _ := ownershipTestService(t)
	parent := ownershipFolder(t, s, own.ID, 0)
	source := filepath.Join(t.TempDir(), "movie.mp4")
	if err := os.WriteFile(source, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	hash, err := helpers.HashFileContext(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: uuid.NewString(), StorageID: "local", Hash: hash, Size: 7}
	if err := s.Deps.DB.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	for _, destination := range []uint{0, parent.ID} {
		if status, _, err := s.CreateFolder("child", destination, own.ID); err != nil || status != http.StatusOK {
			t.Fatalf("folder = (%d,%v)", status, err)
		}
		if status, l, err := s.CloneFileByHash(hash, destination, "movie.mp4", own.ID, ""); err != nil || status != http.StatusOK || l.ParentFolderID != destination {
			t.Fatalf("clone = (%d,%#v,%v)", status, l, err)
		}
		if status, l, cloned, err := s.CreateFileContext(context.Background(), &source, destination, "movie.mp4", "id", 7, own.ID, ""); err != nil || status != http.StatusOK || !cloned || l.ParentFolderID != destination {
			t.Fatalf("file = (%d,%#v,%v,%v)", status, l, cloned, err)
		}
		if status, session, err := s.StageSimpleUpload(destination, "movie.mp4", strings.NewReader("content"), 7, own.ID); err != nil || status != http.StatusAccepted || session.ParentFolderID != destination {
			t.Fatalf("stage = (%d,%#v,%v)", status, session, err)
		}
		if status, link, err := s.SimpleUpload(destination, "movie.mp4", strings.NewReader("content"), 7, own.ID); err != nil || status != http.StatusOK || link.ParentFolderID != destination {
			t.Fatalf("legacy upload = (%d,%#v,%v)", status, link, err)
		}
	}
}

func TestOwnershipCreationKeyChecksExistingOutput(t *testing.T) {
	for _, state := range []string{"foreign parent", "foreign link", "deleted parent", "moved output"} {
		t.Run(state, func(t *testing.T) {
			s, own, foreign := ownershipTestService(t)
			original := ownershipFolder(t, s, own.ID, 0)
			parent := original.ID
			owner := own.ID
			switch state {
			case "foreign parent":
				parent = ownershipFolder(t, s, foreign.ID, 0).ID
			case "foreign link":
				owner = foreign.ID
			case "moved output":
				parent = 0
			}
			l := models.Link{UUID: uuid.NewString(), UserID: owner, ParentFolderID: parent, CreationKey: uploadCreationKey(own.ID, "retry")}
			if err := s.Deps.DB.Create(&l).Error; err != nil {
				t.Fatal(err)
			}
			if state == "deleted parent" || state == "moved output" {
				if err := s.Deps.DB.Delete(&original).Error; err != nil {
					t.Fatal(err)
				}
			}
			status, result, cloned, err := s.CreateFileContext(context.Background(), nil, original.ID, "movie.mp4", "id", 7, own.ID, "retry")
			if state == "moved output" {
				if err != nil || status != http.StatusOK || result.ID != l.ID || !cloned {
					t.Fatalf("safe retry = (%d,%#v,%v,%v)", status, result, cloned, err)
				}
			} else if err == nil || status == http.StatusOK || result != nil {
				t.Fatalf("unsafe retry = (%d,%#v,%v)", status, result, err)
			}
		})
	}
}

func TestOwnershipCloneRevalidatesDeletedParent(t *testing.T) {
	s, own, _ := ownershipTestService(t)
	parent := ownershipFolder(t, s, own.ID, 0)
	file := models.File{UUID: uuid.NewString(), StorageID: "local", Hash: "content", Size: 7}
	if err := s.Deps.DB.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	deleted := false
	if err := s.Deps.DB.Callback().Query().After("gorm:query").Register("test:delete_parent", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && !deleted {
			deleted = true
			if err := tx.Session(&gorm.Session{NewDB: true}).Delete(&parent).Error; err != nil {
				tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	status, l, err := s.CloneFileByHash("content", parent.ID, "movie.mp4", own.ID, "")
	if !deleted || status != http.StatusBadRequest || err == nil || l != nil {
		t.Fatalf("clone after deletion = (%d,%#v,%v), deleted=%v", status, l, err, deleted)
	}
	requireOwnershipCounts(t, s, 0, 0)
}

type ownershipPublishStore struct {
	storage.Store
	afterPut func() error
}

func (s *ownershipPublishStore) Put(ctx context.Context, key storage.Key, r io.Reader, opts storage.PutOptions) (storage.ObjectInfo, error) {
	info, err := s.Store.Put(ctx, key, r, opts)
	if err == nil && s.afterPut != nil {
		err = s.afterPut()
	}
	return info, err
}
func TestOwnershipFileRevalidatesParentAfterPublication(t *testing.T) {
	s, own, _ := ownershipTestService(t)
	parent := ownershipFolder(t, s, own.ID, 0)
	source := filepath.Join(t.TempDir(), "movie.mp4")
	if output, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:d=1:r=1", "-c:v", "mpeg4", source).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, output)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	local, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deletingStore := &ownershipPublishStore{Store: local, afterPut: func() error { return s.Deps.DB.Delete(&parent).Error }}
	service, err := storage.NewService("local", storage.LegacyMediaLayout{}, map[string]storage.Store{"local": deletingStore})
	if err != nil {
		t.Fatal(err)
	}
	s.Deps.Storage = service
	t.Cleanup(func() { _ = service.Close() })
	mount := models.StorageMount{UUID: "local", Mounted: true}
	pool := models.StoragePool{UUID: "pool", IsDefault: true}
	for _, model := range []any{&mount, &pool} {
		if err := s.Deps.DB.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Deps.DB.Create(&models.StoragePoolMount{StoragePoolID: pool.ID, StorageMountID: mount.ID, Role: models.StoragePoolMountPrimary}).Error; err != nil {
		t.Fatal(err)
	}
	fileID := uuid.NewString()
	status, l, _, err := s.CreateFileContext(context.Background(), &source, parent.ID, "movie.mp4", fileID, info.Size(), own.ID, "")
	if status != http.StatusBadRequest || err == nil || l != nil {
		t.Fatalf("file after deletion = (%d,%#v,%v)", status, l, err)
	}
	requireOwnershipCounts(t, s, 0, 0)
	var files int64
	if err := s.Deps.DB.Model(&models.File{}).Count(&files).Error; err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("files=%d", files)
	}
	key, err := service.Layout().Source(fileID, "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deletingStore.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("published source cleanup: %v", err)
	}
}

func TestOwnershipFolderDeletionRejectsEntireMixedTree(t *testing.T) {
	for _, kind := range []string{"empty foreign folder", "foreign link"} {
		t.Run(kind, func(t *testing.T) {
			s, own, foreign := ownershipTestService(t)
			root := ownershipFolder(t, s, own.ID, 0)
			child := ownershipFolder(t, s, own.ID, root.ID)
			link := ownershipLink(t, s, own.ID, root.ID)
			folders, links := int64(2), int64(1)
			if kind == "empty foreign folder" {
				ownershipFolder(t, s, foreign.ID, child.ID)
				folders++
			} else {
				ownershipLink(t, s, foreign.ID, child.ID)
				links++
			}
			status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, false)
			if status != http.StatusForbidden || err == nil {
				t.Fatalf("mixed deletion=(%d,%v)", status, err)
			}
			requireOwnershipCounts(t, s, folders, links)
			key, _ := s.Deps.Storage.Layout().Thumbnail(link.File.UUID, link.Thumbnail)
			store, _ := s.Deps.Storage.Store("local")
			if _, err := store.Stat(context.Background(), key); err != nil {
				t.Fatalf("poster changed: %v", err)
			}
		})
	}
}
func TestOwnershipFolderDeletionAllowsOwnTreeAndAdminCleanup(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "own tree", true: "admin mixed tree"}[admin], func(t *testing.T) {
			s, own, foreign := ownershipTestService(t)
			root := ownershipFolder(t, s, own.ID, 0)
			owner := own.ID
			if admin {
				owner = foreign.ID
			}
			child := ownershipFolder(t, s, owner, root.ID)
			link := ownershipLink(t, s, owner, child.ID)
			status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, admin)
			if status != http.StatusOK || err != nil {
				t.Fatalf("deletion=(%d,%v)", status, err)
			}
			requireOwnershipCounts(t, s, 0, 0)
			key, _ := s.Deps.Storage.Layout().Thumbnail(link.File.UUID, link.Thumbnail)
			store, _ := s.Deps.Storage.Store("local")
			if _, err := store.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("poster cleanup: %v", err)
			}
		})
	}
}
func TestOwnershipFolderDeletionTerminatesCycles(t *testing.T) {
	s, own, _ := ownershipTestService(t)
	root := ownershipFolder(t, s, own.ID, 0)
	child := ownershipFolder(t, s, own.ID, root.ID)
	if err := s.Deps.DB.Model(&root).Update("parent_folder_id", child.ID).Error; err != nil {
		t.Fatal(err)
	}
	ownershipLink(t, s, own.ID, child.ID)
	if status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, false); err != nil || status != http.StatusOK {
		t.Fatalf("cycle deletion=(%d,%v)", status, err)
	}
	requireOwnershipCounts(t, s, 0, 0)
}
func TestOwnershipFolderDeletionPropagatesErrorsAndRollsBack(t *testing.T) {
	for _, failure := range []string{"descendant query", "folder write", "final folder owner", "final link owner"} {
		t.Run(failure, func(t *testing.T) {
			s, own, foreign := ownershipTestService(t)
			root := ownershipFolder(t, s, own.ID, 0)
			ownershipFolder(t, s, own.ID, root.ID)
			link := ownershipLink(t, s, own.ID, root.ID)
			want := http.StatusInternalServerError
			if failure == "descendant query" {
				queries := 0
				if err := s.Deps.DB.Callback().Query().Before("gorm:query").Register("test:query_error", func(tx *gorm.DB) {
					if tx.Statement.Table == "folders" {
						queries++
						if queries == 2 {
							tx.AddError(errors.New("injected query failure"))
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				if strings.HasPrefix(failure, "final") {
					want = http.StatusForbidden
				}
				if err := s.Deps.DB.Callback().Delete().Before("gorm:delete").Register("test:delete_error", func(tx *gorm.DB) {
					if failure == "folder write" && tx.Statement.Table == "folders" {
						tx.AddError(errors.New("injected delete failure"))
					}
					if failure == "final folder owner" && tx.Statement.Table == "folders" {
						tx.Session(&gorm.Session{NewDB: true}).Model(&models.Folder{}).Where("id = ?", root.ID).Update("user_id", foreign.ID)
					}
					if failure == "final link owner" && tx.Statement.Table == "links" {
						tx.Session(&gorm.Session{NewDB: true}).Model(&models.Link{}).Where("id = ?", link.ID).Update("user_id", foreign.ID)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			status, err := s.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{{FolderID: root.ID}}}, own.ID, false)
			if err == nil || status != want {
				t.Fatalf("failed deletion=(%d,%v),want %d", status, err, want)
			}
			requireOwnershipCounts(t, s, 2, 1)
			var persisted models.Link
			if err := s.Deps.DB.First(&persisted, link.ID).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.UserID != own.ID {
				t.Fatal("owner change was not rolled back")
			}
			key, _ := s.Deps.Storage.Layout().Thumbnail(link.File.UUID, link.Thumbnail)
			store, _ := s.Deps.Storage.Store("local")
			if _, err := store.Stat(context.Background(), key); err != nil {
				t.Fatalf("poster cleaned before commit: %v", err)
			}
		})
	}
}

func TestOwnershipCreationPropagatesParentQueryErrors(t *testing.T) {
	for _, method := range []string{"folder", "clone", "file", "legacy upload", "staged upload"} {
		t.Run(method, func(t *testing.T) {
			s, own, _ := ownershipTestService(t)
			parent := ownershipFolder(t, s, own.ID, 0)
			if err := s.Deps.DB.Callback().Query().Before("gorm:query").Register("test:parent_query_error", func(tx *gorm.DB) {
				if tx.Statement.Table == "folders" {
					tx.AddError(errors.New("parent query failed"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			var status int
			var err error
			switch method {
			case "folder":
				status, _, err = s.CreateFolder("child", parent.ID, own.ID)
			case "clone":
				status, _, err = s.CloneFileByHash("hash", parent.ID, "movie.mp4", own.ID, "")
			case "file":
				status, _, _, err = s.CreateFileContext(context.Background(), nil, parent.ID, "movie.mp4", "id", 1, own.ID, "")
			case "legacy upload":
				status, _, err = s.SimpleUpload(parent.ID, "movie.mp4", strings.NewReader("x"), 1, own.ID)
			case "staged upload":
				status, _, err = s.StageSimpleUpload(parent.ID, "movie.mp4", strings.NewReader("x"), 1, own.ID)
			}
			if status != http.StatusInternalServerError || err == nil {
				t.Fatalf("creation=(%d,%v), want query failure", status, err)
			}
		})
	}
}

type ownershipMutationReader struct {
	io.Reader
	mutate func() error
}

func (r *ownershipMutationReader) Read(p []byte) (int, error) {
	if r.mutate != nil {
		fn := r.mutate
		r.mutate = nil
		if err := fn(); err != nil {
			return 0, err
		}
	}
	return r.Reader.Read(p)
}
func TestOwnershipSimpleUploadRevalidatesDestinationAfterBody(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, mutation := range []string{"deleted", "changed owner"} {
			t.Run(map[bool]string{false: "staged ", true: "legacy "}[legacy]+mutation, func(t *testing.T) {
				s, own, foreign := ownershipTestService(t)
				parent := ownershipFolder(t, s, own.ID, 0)
				reader := &ownershipMutationReader{Reader: strings.NewReader("content"), mutate: func() error {
					if mutation == "deleted" {
						return s.Deps.DB.Delete(&parent).Error
					}
					return s.Deps.DB.Model(&parent).Update("user_id", foreign.ID).Error
				}}
				var status int
				var err error
				if legacy {
					status, _, err = s.SimpleUpload(parent.ID, "movie.mp4", reader, 7, own.ID)
				} else {
					status, _, err = s.StageSimpleUpload(parent.ID, "movie.mp4", reader, 7, own.ID)
				}
				if status != http.StatusBadRequest || err == nil {
					t.Fatalf("upload after mutation=(%d,%v)", status, err)
				}
				var sessions int64
				if err := s.Deps.DB.Model(&models.UploadSession{}).Count(&sessions).Error; err != nil {
					t.Fatal(err)
				}
				if sessions != 0 {
					t.Fatalf("orphan sessions=%d", sessions)
				}
				entries, err := os.ReadDir(s.Config().FolderVideoUploadsPriv)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Fatalf("temporary upload leaked: %v", entries)
				}
			})
		}
	}
}
func TestOwnershipAdminMoveRejectsForeignDestination(t *testing.T) {
	for _, kind := range []string{"folder", "link"} {
		t.Run(kind, func(t *testing.T) {
			s, own, foreign := ownershipTestService(t)
			target := ownershipFolder(t, s, foreign.ID, 0)
			var folders, links []uint
			if kind == "folder" {
				folders = []uint{ownershipFolder(t, s, own.ID, 0).ID}
			} else {
				links = []uint{ownershipLink(t, s, own.ID, 0).ID}
			}
			status, err := s.MoveItems(own.ID, target.ID, folders, links, true)
			if status != http.StatusBadRequest || err == nil {
				t.Fatalf("admin cross-owner move=(%d,%v)", status, err)
			}
		})
	}
}
func TestOwnershipDeleteFilesRejectsMixedSelectionAndAllowsAdmin(t *testing.T) {
	s, own, foreign := ownershipTestService(t)
	mine := ownershipLink(t, s, own.ID, 0)
	other := ownershipLink(t, s, foreign.ID, 0)
	validation := &models.LinksDeleteValidation{LinkIDs: []models.LinkDeleteValidation{{LinkID: mine.ID}, {LinkID: other.ID}}}
	if status, err := s.DeleteFiles(validation, own.ID, false); status != http.StatusBadRequest || err == nil {
		t.Fatalf("mixed selection=(%d,%v)", status, err)
	}
	requireOwnershipCounts(t, s, 0, 2)
	if status, err := s.DeleteFiles(validation, own.ID, true); status != http.StatusOK || err != nil {
		t.Fatalf("admin deletion=(%d,%v)", status, err)
	}
	requireOwnershipCounts(t, s, 0, 0)
}
