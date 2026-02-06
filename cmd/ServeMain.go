package cmd

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/routes"
	"ch/kirari04/videocms/services"
)

func ServeMain() {
	Init()

	// sync UserRequestAsync
	helpers.UserRequestAsyncObj.Sync(true)

	// start encoding process
	if *config.ENV.EncodingEnabled {

		services.ResetEncodingState()
		go services.Encoder()
	}

	// start remote downloader
	go services.Downloader()

	// start cleanup process
	go services.EncoderCleanup()
	go services.Deleter()

	// start system resource tracker
	go services.Resources()

	// for setting up the webserver
	inits.Server()

	// for loading the webservers routes
	routes.Api()
	routes.Web()

	// for starting the webserver
	inits.ServerStart()
}
