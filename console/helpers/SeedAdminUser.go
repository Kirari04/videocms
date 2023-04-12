package console_helpers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
)

func SeedAdminUser() error {

	if res := inits.DB.
		Where(&models.User{Username: "admin"}).
		Unscoped().
		Delete(&models.User{}); res.Error != nil {
		return fmt.Errorf("error while deleting existing admin user: %s", res.Error.Error())
	}

	hash, _ := helpers.HashPassword("12345678")
	if res := inits.DB.Create(&models.User{
		Username: "admin",
		Hash:     hash,
		Admin:    true,
	}); res.Error != nil {
		return fmt.Errorf("error while creating admin user: %s", res.Error.Error())
	}

	return nil
}
