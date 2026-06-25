package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/middlewares"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

func Web(app *echo.Echo, handlers *controllers.Handlers, middlewareFactory *middlewares.Factory) {
	cfg := handlers.Config()
	app.Static("/", "public/")

	app.GET("/captcha/challenge", handlers.GetCaptchaChallenge)
	app.POST("/captcha/verify", handlers.VerifyCaptchaChallenge)

	app.GET("/v/:UUID", handlers.PlayerController,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateWeb), cfg.RatelimitBurstWeb, time.Minute*5)))
	app.GET("/v/:UUID/status", handlers.PlayerStatusController,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateWeb), cfg.RatelimitBurstWeb, time.Minute*5)))

	videoData := app.Group(cfg.FolderVideoQualitysPub,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateWeb), cfg.RatelimitBurstWeb, time.Minute*5)))
	videoData.GET("/:UUID/stream/muted/master.m3u8", handlers.GetM3u8Data, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/stream/multi/master.m3u8", handlers.GetM3u8DataMulti, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/image/thumb/:FILE", handlers.GetThumbnailData)
	videoData.GET("/:UUID/:SUBUUID/subtitle/:FILE", handlers.GetSubtitleData, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:AUDIOUUID/stream/master.m3u8", handlers.GetM3u8Data, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:QUALITY/download/video.mkv", handlers.DownloadVideoController, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:QUALITY/:STREAM/stream/video.mp4", handlers.DownloadVideoController, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:QUALITY/:FILE", handlers.GetVideoData, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:AUDIOUUID/audio/:FILE", handlers.GetAudioData, middlewareFactory.MediaAuth())
}
