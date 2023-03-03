package main

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/encworker"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/routes"
)

func main() {
	// setting up required folders and config files
	inits.Folders()
	// for loading the .env file into the application
	inits.Dotenv()
	// for setting up configuration file from env
	config.Setup()
	// for setting up the database connection
	inits.Database()
	// for migrating all the models
	inits.Models()
	// sync UserRequestAsync
	helpers.UserRequestAsyncObj.Sync(true)

	// start encoding process
	encworker.ResetEncodingState()
	go encworker.StartEncode()
	encworker.ResetEncodingState_sub()
	go encworker.StartEncode_sub()
	encworker.ResetEncodingState_audio()
	go encworker.StartEncode_audio()
	// start cleenup process
	go encworker.StartEncCleenup()

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
