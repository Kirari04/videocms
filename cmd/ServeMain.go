package cmd

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/mediacache"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/routes"
	"ch/kirari04/videocms/services"
	"ch/kirari04/videocms/services/tusupload"
	"ch/kirari04/videocms/traffic"
	"context"
	"log"
	"os"
	"time"
)

func ServeMain() {
	deps, err := InitRuntime()
	if err != nil {
		log.Println("failed to initialize runtime:", err)
		os.Exit(1)
	}
	if deps.Storage != nil {
		defer func() {
			if err := deps.Storage.Close(); err != nil {
				log.Printf("failed to close storage: %v", err)
			}
		}()
	}

	// sync UserRequestAsync
	deps.RequestGate.Sync(true)
	trafficRecorder := traffic.NewRecorder(deps.DB, traffic.Options{})
	deps.Traffic = trafficRecorder
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := trafficRecorder.Shutdown(ctx); err != nil {
			log.Printf("failed to flush traffic accounting: %v", err)
		}
	}()

	authSvc := auth.NewService(deps)
	logicSvc := logic.NewService(deps)
	tusSvc := tusupload.NewService(deps, authSvc)
	workerGroup := services.NewWorkerGroup(deps, logicSvc)
	backgroundRuntime := background.New(deps.DB, background.Options{
		Capacity: func(queue string) int {
			cfg := deps.Config()
			switch queue {
			case background.QueueFFmpeg:
				if cfg.MaxParallelFFmpegTasks > 0 {
					return int(cfg.MaxParallelFFmpegTasks)
				}
				if cfg.MaxRunningEncodes > cfg.MaxParallelDownloadPreparations {
					return int(cfg.MaxRunningEncodes)
				}
				return int(cfg.MaxParallelDownloadPreparations)
			case background.QueueNetwork:
				return int(cfg.MaxParallelDownloads)
			case background.QueueStorage:
				return 2
			default:
				return 1
			}
		},
	})
	deps.Background = backgroundRuntime
	deps.MediaCache = mediacache.New(deps.DB, deps.Storage, backgroundRuntime)
	defer deps.MediaCache.Close()
	if err := workerGroup.RegisterBackgroundHandlers(backgroundRuntime, tusSvc); err != nil {
		log.Println("failed to register background handlers:", err)
		return
	}
	handlers := controllers.NewHandlers(deps, authSvc, logicSvc, workerGroup, tusSvc)
	middlewareFactory := middlewares.NewFactory(deps, authSvc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backgroundRuntime.Start(ctx); err != nil {
		log.Println("failed to start background runtime:", err)
		return
	}

	// for setting up the webserver
	server := inits.BuildServer(deps.Config(), middlewareFactory)

	// for loading the webservers routes
	api := server.Group("/api")
	routes.Api(api, handlers, middlewareFactory)
	routes.Web(server, handlers, middlewareFactory)

	workerGroup.Start(ctx)

	// for starting the webserver
	inits.ServerStartFor(server, deps.Config().Host)
	cancel()
	if !backgroundRuntime.Stop(30 * time.Second) {
		log.Println("background runtime did not drain within 30 seconds; active attempts will recover on restart")
	}
	tusSvc.Close()
}
