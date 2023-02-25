package inits

import "ch/kirari04/videocms/models"

func Models() {
	DB.AutoMigrate(&models.User{})
}
