package main

import (
	"ch/kirari04/videocms/config"
	console_helpers "ch/kirari04/videocms/console/helpers"
	"ch/kirari04/videocms/inits"
	"log"
	"os"
)

func main() {
	// loaing default config
	config.Setup()
	//loading folders
	inits.Folders()
	// for setting up the database connection
	inits.Database()

	// load cmd arguments
	argsWithoutProg := os.Args[1:]

	if len(argsWithoutProg) == 0 {
		log.Println("No arguments passed")
		functions()
		return
	}

	// loop over arguments and exec all matching functions
	for _, v := range argsWithoutProg {
		switch v {
		case "seed:adminuser":
			log.Println("running seed:adminuser")
			if err := console_helpers.SeedAdminUser(); err != nil {
				log.Println(err)
			} else {
				log.Println("success seed:adminuser")
			}
		case "database:fresh":
			log.Println("running database:fresh")
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
		default:
			log.Fatal("Bad arguments passed")
			functions()
		}
	}
}

func functions() {
	log.Println("")
	log.Println("Available commands:")
	log.Println("seed:adminuser")
	log.Println("database:fresh")
}
