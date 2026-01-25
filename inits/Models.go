package inits

import (
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

func Models() {
	if DB == nil {
		log.Fatalln("DB is nil while attempting to migrate")
	}
	mustRun(DB.AutoMigrate(&models.User{}))
	mustRun(DB.AutoMigrate(&models.Folder{}))
	mustRun(DB.AutoMigrate(&models.File{}))
	mustRun(DB.AutoMigrate(&models.Link{}))
	mustRun(DB.AutoMigrate(&models.Quality{}))
	mustRun(DB.AutoMigrate(&models.Subtitle{}))
	mustRun(DB.AutoMigrate(&models.Audio{}))
	mustRun(DB.AutoMigrate(&models.UploadSession{}))
	mustRun(DB.AutoMigrate(&models.UploadChunck{}))
	mustRun(DB.AutoMigrate(&models.Webhook{}))
	mustRun(DB.AutoMigrate(&models.WebPage{}))
	mustRun(DB.AutoMigrate(&models.Setting{}))
	mustRun(DB.AutoMigrate(&models.SystemResource{}))
	mustRun(DB.AutoMigrate(&models.Tag{}))
	mustRun(DB.AutoMigrate(&models.TagLinks{}))
	mustRun(DB.AutoMigrate(&models.TrafficLog{}))

	// init default admin user
	// if no user exists
	var count int64
	if err := DB.Model(&models.User{}).Count(&count).Error; err != nil {
		log.Fatalln("Failed to count users: ", err)
		return
	}
	if count == 0 {
		rawhash, _ := bcrypt.GenerateFromPassword([]byte("12345678"), 14)
		user := models.User{
			Username: "admin",
			Hash:     string(rawhash),
			Admin:    true,
			Settings: models.UserSettings{
				WebhooksEnabled: true,
				WebhooksMax:     100,
			},
		}
		if res := DB.Create(&user); res.Error != nil {
			fmt.Printf("error while creating admin user: %s\n", res.Error.Error())
		}
	}
}

func mustRun(err error) {
	if err != nil {
		log.Fatalln("Failed to migrate: ", err)
	}
}
