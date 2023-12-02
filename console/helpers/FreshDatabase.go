package console_helpers

import (
	"bufio"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"fmt"
	"os"
)

func FreshDatabase() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("This wil delete all your files and database.")
	fmt.Print("Confirm with DELETEALL: ")
	msg, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if msg != "DELETEALL\n" {
		fmt.Printf("Skipping because '%s' doesnt match 'DELETEALL'\n", msg)
		return nil
	}

	os.Remove("./database/database.sqlite")
	os.RemoveAll(config.ENV.FolderVideoQualitysPriv)
	os.RemoveAll(config.ENV.FolderVideoUploadsPriv)
	os.MkdirAll(config.ENV.FolderVideoQualitysPriv, 0776)
	os.MkdirAll(config.ENV.FolderVideoUploadsPriv, 0776)

	// migrate
	inits.Folders()
	config.Setup()
	inits.Database()
	inits.Models()

	return nil
}
