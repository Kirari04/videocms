package main

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/routes"
	"ch/kirari04/videocms/services"
	"log"
)

func main() {
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
		log.Panic("")
	}
	//setup captcha
	inits.Captcha()
	// for setting up the database connection
	inits.Database()
	// for migrating all the models
	inits.Models()
	// sync UserRequestAsync
	helpers.UserRequestAsyncObj.Sync(true)

	// start encoding process
	if *config.ENV.EncodingEnabled {

		services.ResetEncodingState()
		go services.Encoder()

		// start cleanup process
		go services.EncoderCleanup()
		go services.Deleter()
	}

	WebServer()
}

func WebServer() {
	// for setting up the webserver
	inits.Server()

	// for loading the webservers routes
	routes.Api()
	routes.Web()

	// for starting the webserver
	inits.ServerStart()
}
