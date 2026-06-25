package inits

import (
	"ch/kirari04/videocms/config"
	"fmt"
	"log"
	"os"
)

func EnsureFolders(env config.Config) error {
	// create folders
	createFolders := []string{"./database", env.FolderVideoQualitysPriv, env.FolderVideoUploadsPriv, "./logs"}
	for _, createFolder := range createFolders {
		if fileInfo, err := os.Stat(createFolder); err != nil || !fileInfo.IsDir() {
			if err := os.MkdirAll(createFolder, 0766); err != nil {
				return fmt.Errorf("failed to generate essential folder %s: %w", createFolder, err)
			}
			log.Printf("Generated folder: %s\n", createFolder)
		}
	}
	return nil
}
