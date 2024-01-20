package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/middlewares"
	"time"

	"github.com/labstack/echo/v4/middleware"
)

func Api() {
	inits.Api.Use(middleware.RateLimiterWithConfig(*helpers.LimiterConfig(10, 500, time.Minute*5)))

	auth := inits.Api.Group("/auth")
	auth.POST("/login",
		controllers.AuthLogin,
		middleware.RateLimiterWithConfig(*helpers.LimiterConfig(1, 2, time.Minute*5)))
	auth.GET("/check",
		controllers.AuthCheck,
		middleware.RateLimiterWithConfig(*helpers.LimiterConfig(1, 2, time.Minute*5)))
	auth.GET("/refresh",
		controllers.AuthRefresh,
		middleware.RateLimiterWithConfig(*helpers.LimiterConfig(1, 2, time.Minute*5)))
	auth.POST("/apikey",
		controllers.AuthApikey,
		middleware.RateLimiterWithConfig(*helpers.LimiterConfig(1, 2, time.Minute*5)),
		middlewares.Auth())

	// Routes that dont require authentication
	inits.Api.GET("/config", controllers.GetConfig)
	inits.Api.GET("/file/example", controllers.GetFileExample)
	inits.Api.GET("/p/pages", controllers.ListPublicWebPage)
	inits.Api.GET("/p/page", controllers.GetPublicWebPage)

	// requires uploadsession jwt inside body
	inits.Api.POST("/pcu/chunck", controllers.CreateUploadChunck)

	// Routes that require to be authenticated
	protectedApi := inits.Api.Group("", middlewares.Auth())
	protectedApi.POST("/folder", controllers.CreateFolder)
	protectedApi.PUT("/folder", controllers.UpdateFolder)
	protectedApi.DELETE("/folder", controllers.DeleteFolder)
	protectedApi.GET("/folders", controllers.ListFolders)
	protectedApi.DELETE("/folders", controllers.DeleteFolders)

	protectedApi.POST("/file", controllers.CreateFile)
	protectedApi.POST("/file/clone", controllers.CloneFile)
	protectedApi.GET("/file", controllers.GetFile)
	protectedApi.PUT("/file", controllers.UpdateFile)
	protectedApi.DELETE("/file", controllers.DeleteFileController)
	protectedApi.GET("/files", controllers.ListFiles)
	protectedApi.DELETE("/files", controllers.DeleteFilesController)

	protectedApi.GET("/account", controllers.GetAccount)
	protectedApi.GET("/account/settings", controllers.GetUserSettingsController)
	protectedApi.PUT("/account/settings", controllers.UpdateUserSettingsController)

	protectedApi.POST("/server", controllers.CreateServer, middlewares.IsAdmin())
	protectedApi.DELETE("/server", controllers.DeleteServer, middlewares.IsAdmin())
	protectedApi.GET("/servers", controllers.ListServers, middlewares.IsAdmin())

	protectedApi.GET("/pages", controllers.ListWebPage, middlewares.IsAdmin())
	protectedApi.GET("/page", controllers.CreateWebPage, middlewares.IsAdmin())
	protectedApi.PUT("/page", controllers.UpdateWebPage, middlewares.IsAdmin())
	protectedApi.DELETE("/page", controllers.DeleteWebPage, middlewares.IsAdmin())

	protectedApi.GET("/stats", controllers.GetSystemStats, middlewares.IsAdmin())
	protectedApi.GET("/settings", controllers.GetSettings, middlewares.IsAdmin())
	protectedApi.PUT("/settings", controllers.UpdateSettings, middlewares.IsAdmin())

	protectedApi.POST("/webhook", controllers.CreateWebhook)
	protectedApi.PUT("/webhook", controllers.UpdateWebhook)
	protectedApi.DELETE("/webhook", controllers.DeleteWebhook)
	protectedApi.GET("/webhooks", controllers.ListWebhooks)

	protectedApi.GET("/encodings", controllers.GetEncodingFiles)

	protectedApi.GET("/pcu/sessions", controllers.GetUploadSessions)
	protectedApi.POST("/pcu/session", controllers.CreateUploadSession)
	protectedApi.DELETE("/pcu/session", controllers.DeleteUploadSession)
	// protectedApi.Post("/pcu/chunck", controllers.CreateUploadChunck)
	protectedApi.POST("/pcu/file", controllers.CreateUploadFile)
}
