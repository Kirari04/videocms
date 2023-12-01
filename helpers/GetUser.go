package helpers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
)

func GetUser(userId uint) (*models.User, error) {
	var user models.User

	if res := inits.DB.First(&user, userId); res.Error != nil {
		return nil, res.Error
	}

	return &user, nil
}
