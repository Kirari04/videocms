package inits

import (
	"log"
	"os"
)

func Folders() {
	// create folders
	createFolders := []string{"./database", "./videos", "./logs"}
	for _, createFolder := range createFolders {
		if fileInfo, err := os.Stat(createFolder); err != nil || !fileInfo.IsDir() {
			os.Mkdir(createFolder, 0777)
		}
	}

	// create env
	if fileInfo, err := os.Stat(".env"); err != nil || fileInfo.IsDir() {
		data, err := os.ReadFile(".env.example")
		if err != nil {
			log.Panic("No .env.example or .env file")
		}
		if err := os.WriteFile(".env", data, 0777); err != nil {
			log.Panic("Failed to generate .env file")
		}
	}
}
