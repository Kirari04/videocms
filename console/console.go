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
		case "create:adminuser":
			log.Println("running create:adminuser")
			if err := console_helpers.CreateAdminUser(); err != nil {
				log.Println(err)
			} else {
				log.Println("success create:adminuser")
			}
		case "migrate":
			log.Println("running migrate")
			if err := console_helpers.Migrate(); err != nil {
				log.Println(err)
			} else {
				log.Println("success migrate")
			}
		case "fresh:database":
			log.Println("running fresh:database")
			if err := console_helpers.FreshDatabase(); err != nil {
				log.Println(err)
			} else {
				log.Println("success fresh:database")
			}
		default:
			log.Fatal("Bad arguments passed")
			functions()
		}
	}
}

func functions() {
	log.Println("")
	log.Println("Available commands:")
	log.Println("create:adminuser")
	log.Println("fresh:database")
	log.Println("migrate")
}
