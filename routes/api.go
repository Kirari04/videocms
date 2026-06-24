package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/middlewares"
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

func Api(apiGroup *echo.Group, handlers *controllers.Handlers, middlewareFactory *middlewares.Factory) {
	cfg := handlers.Config()
	auth := apiGroup.Group("/auth")
	auth.POST("/login",
		handlers.AuthLogin,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateAuth), cfg.RatelimitBurstAuth, time.Minute*5)))
	auth.GET("/check",
		handlers.AuthCheck,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateAuth), cfg.RatelimitBurstAuth, time.Minute*5)))
	auth.GET("/refresh",
		handlers.AuthRefresh,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateAuth), cfg.RatelimitBurstAuth, time.Minute*5)))

	// Routes that dont require authentication
	apiGroup.GET("/config", handlers.GetConfig)
	apiGroup.GET("/file/example", handlers.GetFileExample)
	apiGroup.GET("/p/pages", handlers.ListPublicWebPage)
	apiGroup.GET("/p/page", handlers.GetPublicWebPage)

	// Routes that require to be authenticated
	protectedApi := apiGroup.Group("",
		middlewareFactory.AuthMiddleware(),
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateApi), cfg.RatelimitBurstApi, time.Minute*5)))
	protectedApi.POST("/folder", handlers.CreateFolder)
	protectedApi.PUT("/folder", handlers.UpdateFolder)
	protectedApi.DELETE("/folder", handlers.DeleteFolder)
	protectedApi.PUT("/move", handlers.MoveItems)
	protectedApi.GET("/folders", handlers.ListFolders)
	protectedApi.DELETE("/folders", handlers.DeleteFolders)

	protectedApi.POST("/file", handlers.CreateFile)
	protectedApi.POST("/file/clone", handlers.CloneFile)
	protectedApi.GET("/file", handlers.GetFile)
	protectedApi.PUT("/file", handlers.UpdateFile)
	protectedApi.PUT("/file/thumbnail", handlers.UpdateFileThumbnail)
	protectedApi.DELETE("/file/thumbnail", handlers.DeleteFileThumbnail)
	protectedApi.DELETE("/file", handlers.DeleteFileController)
	protectedApi.GET("/files/search", handlers.SearchFiles)
	protectedApi.GET("/files", handlers.ListFiles)
	protectedApi.DELETE("/files", handlers.DeleteFilesController)
	protectedApi.POST("/file/tag", handlers.CreateTagController)
	protectedApi.DELETE("/file/tag", handlers.DeleteTagController)
	protectedApi.POST("/file/upload", handlers.SimpleUploadController,
		middleware.BodyLimit(fmt.Sprintf("%dk", cfg.MaxUploadFilesize/1024+1024)))

	protectedApi.GET("/account", handlers.GetAccount)
	protectedApi.GET("/account/settings", handlers.GetUserSettingsController)
	protectedApi.PUT("/account/settings", handlers.UpdateUserSettingsController)
	protectedApi.GET("/account/traffic", handlers.GetTrafficStats)
	protectedApi.GET("/account/traffic/top", handlers.GetTopTrafficStats)
	protectedApi.GET("/account/upload", handlers.GetUploadStats)
	protectedApi.GET("/account/upload/top", handlers.GetTopUploadStats)
	protectedApi.GET("/account/encoding", handlers.GetEncodingStats)
	protectedApi.GET("/account/encoding/top", handlers.GetTopEncodingStats)
	protectedApi.GET("/account/storage/top", handlers.GetTopStorageStats)

	protectedApi.GET("/apikeys", handlers.ListApiKeys)
	protectedApi.POST("/apikey", handlers.CreateApiKey)
	protectedApi.DELETE("/apikey/:id", handlers.DeleteApiKey)
	protectedApi.GET("/apikey/:id/audit", handlers.GetApiKeyAudit)

	protectedApi.GET("/pages", handlers.ListWebPage, middlewareFactory.IsAdmin())
	protectedApi.POST("/page", handlers.CreateWebPage, middlewareFactory.IsAdmin())
	protectedApi.PUT("/page", handlers.UpdateWebPage, middlewareFactory.IsAdmin())
	protectedApi.DELETE("/page", handlers.DeleteWebPage, middlewareFactory.IsAdmin())

	protectedApi.GET("/stats", handlers.GetSystemStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/traffic", handlers.GetAdminTrafficStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/traffic/top", handlers.GetAdminTopTrafficStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/upload", handlers.GetAdminUploadStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/upload/top", handlers.GetAdminTopUploadStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/encoding", handlers.GetAdminEncodingStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/encoding/top", handlers.GetAdminTopEncodingStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/storage/top", handlers.GetAdminTopStorageStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/settings", handlers.GetSettings, middlewareFactory.IsAdmin())
	protectedApi.PUT("/settings", handlers.UpdateSettings, middlewareFactory.IsAdmin())

	protectedApi.GET("/users", handlers.GetUsers, middlewareFactory.IsAdmin())
	protectedApi.POST("/users", handlers.CreateUser, middlewareFactory.IsAdmin())
	protectedApi.GET("/users/:id", handlers.GetUser, middlewareFactory.IsAdmin())
	protectedApi.PUT("/users/:id", handlers.UpdateUser, middlewareFactory.IsAdmin())
	protectedApi.DELETE("/users/:id", handlers.DeleteUser, middlewareFactory.IsAdmin())
	protectedApi.POST("/users/:id/password", handlers.ResetUserPassword, middlewareFactory.IsAdmin())

	protectedApi.GET("/admin/encodings", handlers.GetAdminEncodingFiles, middlewareFactory.IsAdmin())

	protectedApi.GET("/versioncheck", handlers.GetVersionCheck, middlewareFactory.IsAdmin())

	protectedApi.POST("/webhook", handlers.CreateWebhook)
	protectedApi.PUT("/webhook", handlers.UpdateWebhook)
	protectedApi.DELETE("/webhook", handlers.DeleteWebhook)
	protectedApi.GET("/webhooks", handlers.ListWebhooks)

	protectedApi.GET("/encodings", handlers.GetEncodingFiles)

	protectedApi.GET("/uploads/sessions", handlers.GetUploadSessions)
	protectedApi.POST("/uploads/:upload_id/finalize", handlers.FinalizeTusUpload)

	// Remote Download
	protectedApi.POST("/remote/download", handlers.CreateRemoteDownload)
	protectedApi.GET("/remote/downloads", handlers.ListRemoteDownloads)
	protectedApi.DELETE("/remote/downloads", handlers.ClearRemoteDownloads)
	protectedApi.POST("/remote/download/:id/cancel", handlers.CancelRemoteDownload)
	protectedApi.POST("/remote/download/:id/retry", handlers.RetryRemoteDownload)
	protectedApi.DELETE("/remote/download/:id", handlers.DeleteRemoteDownload)
	protectedApi.GET("/account/remote-download", handlers.GetRemoteDownloadStats)
	protectedApi.GET("/account/remote-download/duration", handlers.GetRemoteDownloadDurationStats)
	protectedApi.GET("/account/remote-download/top", handlers.GetTopRemoteDownloadStats)

	// Admin Stats
	protectedApi.GET("/stats/remote-download", handlers.GetAdminRemoteDownloadStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/remote-download/duration", handlers.GetAdminRemoteDownloadDurationStats, middlewareFactory.IsAdmin())
	protectedApi.GET("/stats/remote-download/top", handlers.GetAdminTopRemoteDownloadStats, middlewareFactory.IsAdmin())

	uploadMiddlewares := []echo.MiddlewareFunc{
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateUpload), cfg.RatelimitBurstUpload, time.Minute*5)),
		middleware.BodyLimit(fmt.Sprintf("%dk", cfg.MaxUploadChunkSize/1024+1024)),
	}
	apiGroup.Any("/uploads", handlers.TusUpload, uploadMiddlewares...)
	apiGroup.Any("/uploads/*", handlers.TusUpload, uploadMiddlewares...)
}
