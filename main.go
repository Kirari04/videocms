package main

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/encworker"
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
		encworker.ResetEncodingState()
		go encworker.StartEncode()
		encworker.ResetEncodingState_sub()
		go encworker.StartEncode_sub()
		encworker.ResetEncodingState_audio()
		go encworker.StartEncode_audio()
		// start cleenup process
		go encworker.StartEncCleenup()
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
