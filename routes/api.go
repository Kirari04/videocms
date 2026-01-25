package routes

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/middlewares"
	"time"

	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

func Api() {
	inits.Api.Use(middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateGlobal), config.ENV.RatelimitBurstGlobal, time.Minute*5)))

	auth := inits.Api.Group("/auth")
	auth.POST("/login",
		controllers.AuthLogin,
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateAuth), config.ENV.RatelimitBurstAuth, time.Minute*5)))
	auth.GET("/check",
		controllers.AuthCheck,
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateAuth), config.ENV.RatelimitBurstAuth, time.Minute*5)))
	auth.GET("/refresh",
		controllers.AuthRefresh,
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateAuth), config.ENV.RatelimitBurstAuth, time.Minute*5)))
	auth.POST("/apikey",
		controllers.AuthApikey,
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateAuth), config.ENV.RatelimitBurstAuth, time.Minute*5)),
		middlewares.Auth())

	// Routes that dont require authentication
	inits.Api.GET("/config", controllers.GetConfig)
	inits.Api.GET("/file/example", controllers.GetFileExample)
	inits.Api.GET("/p/pages", controllers.ListPublicWebPage)
	inits.Api.GET("/p/page", controllers.GetPublicWebPage)

	// requires uploadsession jwt inside body
	inits.Api.POST("/pcu/chunck", controllers.CreateUploadChunck)

	// Routes that require to be authenticated
	protectedApi := inits.Api.Group("",
		middlewares.Auth(),
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateApi), config.ENV.RatelimitBurstApi, time.Minute*5)))
	protectedApi.POST("/folder", controllers.CreateFolder)
	protectedApi.PUT("/folder", controllers.UpdateFolder)
	protectedApi.DELETE("/folder", controllers.DeleteFolder)
	protectedApi.PUT("/move", controllers.MoveItems)
	protectedApi.GET("/folders", controllers.ListFolders)
	protectedApi.DELETE("/folders", controllers.DeleteFolders)

	protectedApi.POST("/file", controllers.CreateFile)
	protectedApi.POST("/file/clone", controllers.CloneFile)
	protectedApi.GET("/file", controllers.GetFile)
	protectedApi.PUT("/file", controllers.UpdateFile)
	protectedApi.DELETE("/file", controllers.DeleteFileController)
	protectedApi.GET("/files/search", controllers.SearchFiles)
	protectedApi.GET("/files", controllers.ListFiles)
	protectedApi.DELETE("/files", controllers.DeleteFilesController)
	protectedApi.POST("/file/tag", controllers.CreateTagController)
	protectedApi.DELETE("/file/tag", controllers.DeleteTagController)

	protectedApi.GET("/account", controllers.GetAccount)
	protectedApi.GET("/account/settings", controllers.GetUserSettingsController)
	protectedApi.PUT("/account/settings", controllers.UpdateUserSettingsController)
	protectedApi.GET("/account/traffic", controllers.GetTrafficStats)
	protectedApi.GET("/account/traffic/top", controllers.GetTopTrafficStats)
	protectedApi.GET("/account/upload", controllers.GetUploadStats)
	protectedApi.GET("/account/upload/top", controllers.GetTopUploadStats)
	protectedApi.GET("/account/encoding", controllers.GetEncodingStats)
	protectedApi.GET("/account/encoding/top", controllers.GetTopEncodingStats)
	protectedApi.GET("/account/storage/top", controllers.GetTopStorageStats)

	protectedApi.GET("/pages", controllers.ListWebPage, middlewares.IsAdmin())
	protectedApi.POST("/page", controllers.CreateWebPage, middlewares.IsAdmin())
	protectedApi.PUT("/page", controllers.UpdateWebPage, middlewares.IsAdmin())
	protectedApi.DELETE("/page", controllers.DeleteWebPage, middlewares.IsAdmin())

	protectedApi.GET("/stats", controllers.GetSystemStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/traffic", controllers.GetAdminTrafficStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/traffic/top", controllers.GetAdminTopTrafficStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/upload", controllers.GetAdminUploadStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/upload/top", controllers.GetAdminTopUploadStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/encoding", controllers.GetAdminEncodingStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/encoding/top", controllers.GetAdminTopEncodingStats, middlewares.IsAdmin())
	protectedApi.GET("/stats/storage/top", controllers.GetAdminTopStorageStats, middlewares.IsAdmin())
	protectedApi.GET("/settings", controllers.GetSettings, middlewares.IsAdmin())
	protectedApi.PUT("/settings", controllers.UpdateSettings, middlewares.IsAdmin())

	protectedApi.GET("/users", controllers.GetUsers, middlewares.IsAdmin())
	protectedApi.POST("/users", controllers.CreateUser, middlewares.IsAdmin())
	protectedApi.GET("/users/:id", controllers.GetUser, middlewares.IsAdmin())
	protectedApi.PUT("/users/:id", controllers.UpdateUser, middlewares.IsAdmin())
	protectedApi.DELETE("/users/:id", controllers.DeleteUser, middlewares.IsAdmin())
	protectedApi.POST("/users/:id/password", controllers.ResetUserPassword, middlewares.IsAdmin())

	protectedApi.GET("/admin/encodings", controllers.GetAdminEncodingFiles, middlewares.IsAdmin())

	protectedApi.GET("/versioncheck", controllers.GetVersionCheck, middlewares.IsAdmin())

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
