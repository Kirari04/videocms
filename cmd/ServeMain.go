package cmd

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/routes"
	"ch/kirari04/videocms/services"
	"ch/kirari04/videocms/services/tusupload"
	"context"
	"log"
	"os"
)

func ServeMain() {
	deps, err := InitRuntime()
	if err != nil {
		log.Println("failed to initialize runtime:", err)
		os.Exit(1)
	}

	// sync UserRequestAsync
	deps.RequestGate.Sync(true)

	authSvc := auth.NewService(deps)
	logicSvc := logic.NewService(deps)
	tusSvc := tusupload.NewService(deps, authSvc)
	workerGroup := services.NewWorkerGroup(deps, logicSvc)
	handlers := controllers.NewHandlers(deps, authSvc, logicSvc, workerGroup, tusSvc)
	middlewareFactory := middlewares.NewFactory(deps, authSvc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// for setting up the webserver
	server := inits.BuildServer(deps.Config(), middlewareFactory)

	// for loading the webservers routes
	api := server.Group("/api")
	routes.Api(api, handlers, middlewareFactory)
	routes.Web(server, handlers, middlewareFactory)

	workerGroup.Start(ctx)
	go tusSvc.StartCleanup(ctx)

	// for starting the webserver
	inits.ServerStartFor(server, deps.Config().Host)
	cancel()
	tusSvc.Close()
}
