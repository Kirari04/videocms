package console_helpers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
)

func MigrateUserSettings() error {
	var users []models.User
	if res := inits.DB.Find(&users); res.Error != nil {
		return res.Error
	}
	for _, user := range users {
		if user.Settings.UploadSessionsMax == 0 {
			user.Settings.UploadSessionsMax = config.ENV.MaxUploadSessions
		}

		if res := inits.DB.Save(&user); res.Error != nil {
			return res.Error
		}
	}
	return nil
}
