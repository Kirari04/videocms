package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/middlewares"
	"time"

	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func Api() {
	inits.Api.Use(limiter.New(*helpers.LimiterConfig(10, time.Second)))

	auth := inits.Api.Group("/auth")
	auth.Use(limiter.New(*helpers.LimiterConfig(10, time.Hour))).
		Post("/login", controllers.AuthLogin)
	auth.Use(limiter.New(*helpers.LimiterConfig(1, time.Second*10))).
		Get("/check", controllers.AuthCheck)
	auth.Use(limiter.New(*helpers.LimiterConfig(1, time.Minute))).
		Get("/refresh", controllers.AuthRefresh)

	protectedApi := inits.Api.Use(middlewares.Auth)
	protectedApi.Post("/folder", controllers.CreateFolder)
	protectedApi.Delete("/folder", controllers.DeleteFolder)
	protectedApi.Get("/folders", controllers.ListFolders)
	protectedApi.Delete("/folders", controllers.DeleteFolders)

	protectedApi.Post("/file", controllers.CreateFile)
	protectedApi.Get("/file", controllers.GetFile)
	protectedApi.Put("/file", controllers.UpdateFolder)
	protectedApi.Delete("/file", controllers.DeleteFileController)
	protectedApi.Get("/files", controllers.ListFiles)
	protectedApi.Delete("/files", controllers.DeleteFilesController)
}
