package inits

import (
	"ch/kirari04/videocms/config"
	"log"
	"os"
)

func Folders() {
	// create folders
	createFolders := []string{"./database", config.ENV.FolderVideoQualitysPriv, config.ENV.FolderVideoUploadsPriv, "./logs"}
	for _, createFolder := range createFolders {
		if fileInfo, err := os.Stat(createFolder); err != nil || !fileInfo.IsDir() {
			if err := os.MkdirAll(createFolder, 0766); err != nil {
				log.Panicf("Failed to generate essential folder: %s", createFolder)
			}
			log.Printf("Generated folder: %s\n", createFolder)
		}
	}
}
