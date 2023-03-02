package inits

import "ch/kirari04/videocms/models"

func Models() {
	DB.AutoMigrate(&models.User{})
	DB.AutoMigrate(&models.Folder{})
	DB.AutoMigrate(&models.File{})
	DB.AutoMigrate(&models.Link{})
	DB.AutoMigrate(&models.Quality{})
	DB.AutoMigrate(&models.Subtitle{})
	DB.AutoMigrate(&models.Audio{})
}
