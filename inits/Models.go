package inits

import (
	"ch/kirari04/videocms/models"
	"log"
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
	mustRun(DB.AutoMigrate(&models.Server{}))
}

func mustRun(err error) {
	if err != nil {
		log.Fatalln("Failed to migrate: ", err)
	}
}
