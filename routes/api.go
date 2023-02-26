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

	folders := inits.Api.Use(middlewares.Auth).Group("/folders")
	folders.Get("/list", controllers.ListFolders)
	folders.Post("/create", controllers.CreateFolder)
	folders.Delete("/delete", controllers.DeleteFolder)

	files := inits.Api.Use(middlewares.Auth).Group("/files")
	files.Post("/create", controllers.CreateFile)
	files.Get("/list", controllers.ListFiles)
}
