package main

import (
	"ch/kirari04/videocms/encworker"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/routes"
)

func main() {
	// setting up required folders and config files
	inits.Folders()
	// for loading the .env file into the application
	inits.Dotenv()
	// for setting up the database connection
	inits.Database()
	// for migrating all the models
	inits.Models()

	// start encoding process
	encworker.ResetEncodingState()
	go encworker.StartEncode()
	WebServer()
}

func WebServer() {
	// for setting up the webserver
	inits.Server()

	// for loading the webservers routes
	routes.Web()
	routes.Api()

	// for starting the webserver
	inits.ServerStart()
}
