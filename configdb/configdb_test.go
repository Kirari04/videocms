package configdb_test

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/configdb"
	"ch/kirari04/videocms/models"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var legacySigningKeys = []string{
	"BpLnfgDsc2WD8F2qNfHK5a84jjJkwzDkh9h2fhfUVuS9jZ8uVbhV3vC5AWX39IVU",
	"WSP2NcHciWvqZTa2N95RxRTZHWUsaD6HEdz0ThbXfQ6pYSQ3n267l1VQKGNbSuJE",
}

func newSettingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true, IgnoreRelationshipsWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Setting{}, &models.User{}, &models.RevokedAuthSession{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSigningDefaultsAreIndependentAndPersisted(t *testing.T) {
	firstDB, secondDB := newSettingsDB(t), newSettingsDB(t)
	first, err := configdb.LoadSnapshot(firstDB, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := configdb.LoadSnapshot(secondDB, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Config.JwtSecretKey == second.Config.JwtSecretKey || first.Config.JwtMediaSecretKey == second.Config.JwtMediaSecretKey || first.Config.JwtSecretKey == first.Config.JwtMediaSecretKey {
		t.Fatal("signing defaults are not independent")
	}
	for _, key := range []string{first.Config.JwtSecretKey, first.Config.JwtMediaSecretKey} {
		if len(key) < 64 {
			t.Fatal("signing key is too short")
		}
		for _, legacy := range legacySigningKeys {
			if key == legacy {
				t.Fatal("known predictable signing key generated")
			}
		}
	}
	again, err := configdb.LoadSnapshot(firstDB, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Config.JwtSecretKey != again.Config.JwtSecretKey || first.Config.JwtMediaSecretKey != again.Config.JwtMediaSecretKey {
		t.Fatal("reload changed signing keys")
	}
}

func TestSigningDefaultsRotateKnownWeakKeysAndRejectForgedTokens(t *testing.T) {
	for _, legacy := range legacySigningKeys {
		for _, column := range []string{"jwt_secret_key", "jwt_media_secret_key"} {
			t.Run(column+legacy[:4], func(t *testing.T) {
				db := newSettingsDB(t)
				original, err := configdb.LoadSnapshot(db, config.Config{})
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&models.Setting{}).Where("id > 0").Update(column, legacy).Error; err != nil {
					t.Fatal(err)
				}
				current, err := configdb.LoadSnapshot(db, config.Config{})
				if err != nil {
					t.Fatal(err)
				}
				key := current.Config.JwtSecretKey
				if column == "jwt_media_secret_key" {
					key = current.Config.JwtMediaSecretKey
				}
				if key == legacy {
					t.Fatal("persisted predictable key survived upgrade")
				}
				if column == "jwt_secret_key" && current.Config.JwtMediaSecretKey != original.Config.JwtMediaSecretKey || column == "jwt_media_secret_key" && current.Config.JwtSecretKey != original.Config.JwtSecretKey {
					t.Fatal("upgrade changed a custom key")
				}
				user := models.User{Username: "admin", Admin: true}
				if err := db.Create(&user).Error; err != nil {
					t.Fatal(err)
				}
				svc := auth.NewService(&app.Deps{DB: db, Snapshots: app.NewSnapshotStore(current)})
				if column == "jwt_secret_key" {
					forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"userid": user.ID, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(legacy))
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := svc.VerifyJWT(forged); err == nil {
						t.Fatal("forged legacy token accepted after upgrade")
					}
					valid, _, err := svc.GenerateJWT(user)
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := svc.VerifyJWT(valid); err != nil {
						t.Fatalf("legitimate login failed: %v", err)
					}
				} else {
					forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"link_uuid": "link", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(legacy))
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := svc.VerifyMediaToken(forged); err == nil {
						t.Fatal("forged legacy media token accepted")
					}
				}
			})
		}
	}
}

func TestFailedSnapshotDoesNotPersistPartialDefaults(t *testing.T) {
	db := newSettingsDB(t)
	if err := db.Create(&models.Setting{AppName: strings.Repeat("a", 121)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := configdb.LoadSnapshot(db, config.Config{}); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	var row models.Setting
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.JwtSecretKey != "" || row.JwtMediaSecretKey != "" {
		t.Fatal("failed load persisted signing keys")
	}
}
