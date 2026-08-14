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

	// Unified background-work API. User routes are owner-scoped; the central
	// operations console is protected by the admin middleware.
	v2 := protectedApi.Group("/v2")
	v2.GET("/jobs", handlers.ListMyBackgroundJobs)
	v2.GET("/jobs/:id", handlers.GetMyBackgroundJob)
	v2.POST("/jobs/:id/cancel", handlers.CancelMyBackgroundJob)
	v2.POST("/jobs/:id/retry", handlers.RetryMyBackgroundJob)
	// Versioned submission endpoints return 202 + Location and can be followed
	// through the generic jobs API above.
	v2.POST("/uploads/simple", handlers.SimpleUploadController,
		middleware.BodyLimit(fmt.Sprintf("%dk", cfg.MaxUploadFilesize/1024+1024)))
	v2.POST("/uploads/:upload_id/finalize", handlers.FinalizeTusUpload)
	v2.POST("/remote-downloads", handlers.CreateRemoteDownloadV2)
	v2.GET("/remote-downloads", handlers.ListRemoteDownloads)
	v2.DELETE("/remote-downloads", handlers.ClearRemoteDownloads)
	v2.POST("/remote-downloads/:id/cancel", handlers.CancelRemoteDownload)
	v2.POST("/remote-downloads/:id/retry", handlers.RetryRemoteDownload)
	v2.DELETE("/remote-downloads/:id", handlers.DeleteRemoteDownload)
	v2.DELETE("/file", handlers.DeleteFileController)
	v2.DELETE("/files", handlers.DeleteFilesController)
	v2.DELETE("/folder", handlers.DeleteFolder)
	v2.DELETE("/folders", handlers.DeleteFolders)
	adminV2 := v2.Group("/admin", middlewareFactory.IsAdmin())
	adminV2.GET("/jobs", handlers.ListAdminBackgroundJobs)
	adminV2.GET("/jobs/summary", handlers.GetAdminBackgroundSummary)
	adminV2.GET("/jobs/:id", handlers.GetAdminBackgroundJob)
	adminV2.POST("/jobs/:id/cancel", handlers.CancelAdminBackgroundJob)
	adminV2.POST("/jobs/:id/retry", handlers.RetryAdminBackgroundJob)
	adminV2.POST("/tasks/:id/cancel", handlers.CancelAdminBackgroundTask)
	adminV2.POST("/tasks/:id/retry", handlers.RetryAdminBackgroundTask)
	adminV2.GET("/task-queues", handlers.ListAdminBackgroundQueues)
	adminV2.POST("/task-queues/:name/pause", handlers.PauseAdminBackgroundQueue)
	adminV2.POST("/task-queues/:name/resume", handlers.ResumeAdminBackgroundQueue)
	adminV2.GET("/task-schedules", handlers.ListAdminBackgroundSchedules)
	adminV2.POST("/task-schedules/:key/run", handlers.RunAdminBackgroundSchedule)
	adminV2.GET("/task-runtime", handlers.GetAdminBackgroundRuntime)
	protectedApi.POST("/folder", handlers.CreateFolder)
	protectedApi.PUT("/folder", handlers.UpdateFolder)
	protectedApi.DELETE("/folder", handlers.DeleteFolderLegacy)
	protectedApi.PUT("/move", handlers.MoveItems)
	protectedApi.GET("/folders", handlers.ListFolders)
	protectedApi.DELETE("/folders", handlers.DeleteFoldersLegacy)

	protectedApi.POST("/file", handlers.CreateFile)
	protectedApi.POST("/file/clone", handlers.CloneFile)
	protectedApi.GET("/file", handlers.GetFile)
	protectedApi.PUT("/file", handlers.UpdateFile)
	protectedApi.PUT("/file/thumbnail", handlers.UpdateFileThumbnail)
	protectedApi.DELETE("/file/thumbnail", handlers.DeleteFileThumbnail)
	protectedApi.DELETE("/file", handlers.DeleteFileLegacy)
	protectedApi.GET("/files/search", handlers.SearchFiles)
	protectedApi.GET("/files", handlers.ListFiles)
	protectedApi.DELETE("/files", handlers.DeleteFilesLegacy)
	protectedApi.POST("/file/tag", handlers.CreateTagController)
	protectedApi.DELETE("/file/tag", handlers.DeleteTagController)
	protectedApi.POST("/file/upload", handlers.SimpleUploadLegacy,
		middleware.BodyLimit(fmt.Sprintf("%dk", cfg.MaxUploadFilesize/1024+1024)))

	protectedApi.GET("/account", handlers.GetAccount)
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
	protectedApi.POST("/page/preview", handlers.PreviewWebPage, middlewareFactory.IsAdmin())
	protectedApi.GET("/page/:id", handlers.GetWebPage, middlewareFactory.IsAdmin())
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
	protectedApi.POST("/settings/test-pgs-server", handlers.TestPgsServerConnection, middlewareFactory.IsAdmin())
	protectedApi.GET("/admin/storage", handlers.GetStorageAdminOverview, middlewareFactory.IsAdmin())
	protectedApi.POST("/admin/storage/mounts", handlers.CreateStorageMount, middlewareFactory.IsAdmin())
	protectedApi.PUT("/admin/storage/mounts/:id", handlers.UpdateStorageMount, middlewareFactory.IsAdmin())
	protectedApi.DELETE("/admin/storage/mounts/:id", handlers.UnmountStorageMount, middlewareFactory.IsAdmin())
	protectedApi.POST("/admin/storage/mounts/:id/remount", handlers.RemountStorageMount, middlewareFactory.IsAdmin())
	protectedApi.POST("/admin/storage/mounts/:id/check", handlers.CheckStorageMount, middlewareFactory.IsAdmin())
	protectedApi.POST("/admin/storage/mounts/:id/reconnect", handlers.ReconnectStorageMount, middlewareFactory.IsAdmin())
	protectedApi.POST("/admin/storage/pools", handlers.CreateStoragePool, middlewareFactory.IsAdmin())
	protectedApi.PUT("/admin/storage/pools/:id", handlers.UpdateStoragePool, middlewareFactory.IsAdmin())
	protectedApi.DELETE("/admin/storage/pools/:id", handlers.DeleteStoragePool, middlewareFactory.IsAdmin())
	protectedApi.POST("/admin/storage/pools/:id/default", handlers.SetDefaultStoragePool, middlewareFactory.IsAdmin())

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
	protectedApi.POST("/uploads/:upload_id/finalize", handlers.FinalizeTusUploadLegacy)

	// Remote Download
	protectedApi.POST("/remote/download", handlers.CreateRemoteDownloadLegacy)
	protectedApi.GET("/remote/downloads", handlers.ListRemoteDownloads)
	protectedApi.DELETE("/remote/downloads", handlers.ClearRemoteDownloads)
	protectedApi.POST("/remote/download/:id/cancel", handlers.CancelRemoteDownloadLegacy)
	protectedApi.POST("/remote/download/:id/retry", handlers.RetryRemoteDownloadLegacy)
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
