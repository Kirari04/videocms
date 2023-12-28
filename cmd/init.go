package cmd

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"log"
	"os"
)

func Init() {
	// for setting up configuration file from env
	config.Setup()
	// setting up required folders and config files
	inits.Folders()
	// checking env
	if errors := helpers.ValidateStruct(config.ENV); len(errors) > 0 {
		log.Println("Invalid Env configuration;")
		for _, err := range errors {
			log.Printf("%v", err)
		}
		os.Exit(1)
	}
	//setup captcha
	inits.Captcha()
	// for setting up the database connection
	inits.Database()
	// for migrating all the models
	inits.Models()
}
