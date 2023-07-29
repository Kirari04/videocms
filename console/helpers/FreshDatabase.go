package console_helpers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"os"
)

func FreshDatabase() error {
	os.Remove("./database/database.sqlite")
	os.RemoveAll(config.ENV.FolderVideoQualitysPriv)
	os.RemoveAll(config.ENV.FolderVideoUploadsPriv)
	os.MkdirAll(config.ENV.FolderVideoQualitysPriv, 0776)
	os.MkdirAll(config.ENV.FolderVideoUploadsPriv, 0776)
	os.Create("./database/database.sqlite")

	// migrate
	inits.Folders()
	config.Setup()
	inits.Database()
	inits.Models()

	if err := SeedAdminUser(); err != nil {
		return err
	}

	return nil
}
