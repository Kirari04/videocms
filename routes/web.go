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

func Web() {
	inits.App.Static("/", "public/")

	inits.App.GET("/captcha/challenge", controllers.GetCaptchaChallenge)
	inits.App.POST("/captcha/verify", controllers.VerifyCaptchaChallenge)

	inits.App.GET("/v/:UUID", controllers.PlayerController,
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateWeb), config.ENV.RatelimitBurstWeb, time.Minute*5)))

	videoData := inits.App.Group(config.ENV.FolderVideoQualitysPub,
		middleware.RateLimiterWithConfig(*middlewares.LimiterConfig(rate.Limit(config.ENV.RatelimitRateWeb), config.ENV.RatelimitBurstWeb, time.Minute*5)))
	videoData.GET("/:UUID/stream/muted/master.m3u8", controllers.GetM3u8Data, middlewares.JwtStream())
	videoData.GET("/:UUID/stream/multi/master.m3u8", controllers.GetM3u8DataMulti, middlewares.JwtStream())
	videoData.GET("/:UUID/image/thumb/:FILE", controllers.GetThumbnailData)
	videoData.GET("/:UUID/:SUBUUID/subtitle/:FILE", controllers.GetSubtitleData, middlewares.JwtStream())
	videoData.GET("/:UUID/:AUDIOUUID/stream/master.m3u8", controllers.GetM3u8Data, middlewares.JwtStream())
	videoData.GET("/:UUID/:QUALITY/download/video.mkv", controllers.DownloadVideoController, middlewares.JwtStream())
	videoData.GET("/:UUID/:QUALITY/:JWT/:STREAM/stream/video.mp4", controllers.DownloadVideoController, middlewares.JwtStream())
	// no jwt stream
	videoData.GET("/:UUID/:QUALITY/:FILE", controllers.GetVideoData)
	videoData.GET("/:UUID/:AUDIOUUID/audio/:FILE", controllers.GetAudioData)
}
