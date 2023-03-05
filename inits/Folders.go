package inits

import (
	"ch/kirari04/videocms/config"
	"log"
	"os"
)

func Folders() {
	// create folders
	createFolders := []string{"./database", "./videos", "./logs"}
	for _, createFolder := range createFolders {
		if fileInfo, err := os.Stat(createFolder); err != nil || !fileInfo.IsDir() {
			if err := os.Mkdir(createFolder, 0777); err != nil {
				log.Panic("Failed to generate essential folder")
			}
			log.Printf("Generated folder: %s\n", createFolder)
		}
	}

	// create .env.example
	if fileInfo, err := os.Stat(".env.example"); err != nil || fileInfo.IsDir() {
		if err != nil {
			log.Println("No existing .env.example file")
			log.Println("Using Go default env data to populate .env.example")
			config.Setup()
			data := []byte(config.ENV.String())
			// generate env example file
			if err := os.WriteFile(".env.example", data, 0777); err != nil {
				log.Panic("Failed to generate .env.example file")
			}
		}
	}
}
