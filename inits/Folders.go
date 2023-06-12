package inits

import (
	"log"
	"os"
)

func Folders() {
	// create folders
	createFolders := []string{"./database", "./videos", "./videos/uploads", "./logs"}
	for _, createFolder := range createFolders {
		if fileInfo, err := os.Stat(createFolder); err != nil || !fileInfo.IsDir() {
			if err := os.Mkdir(createFolder, 0766); err != nil {
				log.Panic("Failed to generate essential folder")
			}
			log.Printf("Generated folder: %s\n", createFolder)
		}
	}
}
