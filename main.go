package main

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/routes"
)

func main() {
	// for loading the .env file into the application
	inits.Dotenv()
	// for setting up the database connection
	inits.Database()
	// for migrating all the models
	inits.Models()

	// for setting up the webserver
	inits.Server()

	// for loading the webservers routes
	routes.Web()
	routes.Api()

	// for starting the webserver
	inits.ServerStart()
}
