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
			if err := os.Mkdir(createFolder, 0777); err != nil {
				log.Panic("Failed to generate essential folder")
			}
			log.Printf("Generated folder: %s\n", createFolder)
		}
	}

	// create env
	if fileInfo, err := os.Stat(".env"); err != nil || fileInfo.IsDir() {
		data, err := os.ReadFile(".env.example")
		if err != nil {
			log.Println("No .env.example or .env file")
			log.Println("Using Go defual env data")
			data = []byte(
				"AppName=VideoCMS\n" +
					"Host=:3000\n" +
					"secretKey=sampleSecretKeyForAuthentication\n",
			)
		}
		if err := os.WriteFile(".env", data, 0777); err != nil {
			log.Panic("Failed to generate .env file")
		}
		log.Println("Generated .env file from .env.example")
	}
}
