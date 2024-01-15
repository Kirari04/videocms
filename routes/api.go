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

	// Routes that dont require authentication
	inits.Api.Get("/config", controllers.GetConfig)
	inits.Api.Get("/file/example", controllers.GetFileExample)
	inits.Api.Get("/p/pages", controllers.ListPublicWebPage)
	inits.Api.Get("/p/page", controllers.GetPublicWebPage)

	// requires uploadsession jwt inside body
	inits.Api.Post("/pcu/chunck", controllers.CreateUploadChunck)

	// Routes that require to be authenticated
	protectedApi := inits.Api.Group("", middlewares.Auth)
	protectedApi.Post("/folder", controllers.CreateFolder)
	protectedApi.Put("/folder", controllers.UpdateFolder)
	protectedApi.Delete("/folder", controllers.DeleteFolder)
	protectedApi.Get("/folders", controllers.ListFolders)
	protectedApi.Delete("/folders", controllers.DeleteFolders)

	protectedApi.Post("/file", controllers.CreateFile)
	protectedApi.Post("/file/clone", controllers.CloneFile)
	protectedApi.Get("/file", controllers.GetFile)
	protectedApi.Put("/file", controllers.UpdateFile)
	protectedApi.Delete("/file", controllers.DeleteFileController)
	protectedApi.Get("/files", controllers.ListFiles)
	protectedApi.Delete("/files", controllers.DeleteFilesController)

	protectedApi.Get("/account", controllers.GetAccount)
	protectedApi.Get("/account/settings", controllers.GetUserSettingsController)
	protectedApi.Put("/account/settings", controllers.UpdateUserSettingsController)

	protectedApi.Post("/server", middlewares.IsAdmin, controllers.CreateServer)
	protectedApi.Delete("/server", middlewares.IsAdmin, controllers.DeleteServer)
	protectedApi.Get("/servers", middlewares.IsAdmin, controllers.ListServers)

	protectedApi.Get("/pages", middlewares.IsAdmin, controllers.ListWebPage)
	protectedApi.Post("/page", middlewares.IsAdmin, controllers.CreateWebPage)
	protectedApi.Put("/page", middlewares.IsAdmin, controllers.UpdateWebPage)
	protectedApi.Delete("/page", middlewares.IsAdmin, controllers.DeleteWebPage)

	protectedApi.Get("/stats", middlewares.IsAdmin, controllers.GetSystemStats)

	protectedApi.Post("/webhook", controllers.CreateWebhook)
	protectedApi.Put("/webhook", controllers.UpdateWebhook)
	protectedApi.Delete("/webhook", controllers.DeleteWebhook)
	protectedApi.Get("/webhooks", controllers.ListWebhooks)

	protectedApi.Get("/encodings", controllers.GetEncodingFiles)

	protectedApi.Get("/pcu/sessions", controllers.GetUploadSessions)
	protectedApi.Post("/pcu/session", controllers.CreateUploadSession)
	protectedApi.Delete("/pcu/session", controllers.DeleteUploadSession)
	// protectedApi.Post("/pcu/chunck", controllers.CreateUploadChunck)
	protectedApi.Post("/pcu/file", controllers.CreateUploadFile)
}
