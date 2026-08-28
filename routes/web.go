package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/middlewares"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

func Web(app *echo.Echo, handlers *controllers.Handlers, middlewareFactory *middlewares.Factory) {
	cfg := handlers.Config()
	publicFS := echo.MustSubFS(app.Filesystem, "public")
	app.GET(
		"/*",
		echo.StaticDirectoryHandler(publicFS, !app.EnablePathUnescapingStaticFiles),
		frontendCacheControl,
	)

	app.GET("/captcha/challenge", handlers.GetCaptchaChallenge)
	app.POST("/captcha/verify", handlers.VerifyCaptchaChallenge)

	app.GET("/v/:UUID", handlers.PlayerController,
		middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(cfg.RatelimitRateWeb), cfg.RatelimitBurstWeb, time.Minute*5)))
	app.GET("/v/:UUID/download", handlers.DownloadPageController,
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
	videoData.POST("/:UUID/download-jobs", handlers.CreateDownloadJob, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/download-jobs/:JOBUUID", handlers.GetDownloadJob, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/download-jobs/:JOBUUID/file", handlers.DownloadPreparedFile, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:QUALITY/:STREAM/stream/video.mp4", handlers.DownloadVideoController, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:QUALITY/:FILE", handlers.GetVideoData, middlewareFactory.MediaAuth())
	videoData.GET("/:UUID/:AUDIOUUID/audio/:FILE", handlers.GetAudioData, middlewareFactory.MediaAuth())
}

const (
	frontendRevalidateCacheControl = "no-cache, must-revalidate"
	frontendImmutableCacheControl  = "public, max-age=31536000, immutable"
)

func frontendCacheControl(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", frontendCacheControlForPath(c.Request().URL.Path))
		if err := next(c); err != nil {
			// A missing fingerprinted asset may fall back to the SPA shell. Never
			// cache that fallback as if it were the requested immutable asset.
			c.Response().Header().Set("Cache-Control", frontendRevalidateCacheControl)
			return err
		}
		return nil
	}
}

func frontendCacheControlForPath(requestPath string) string {
	if strings.HasPrefix(requestPath, "/_nuxt/") &&
		!strings.HasPrefix(requestPath, "/_nuxt/builds/") {
		return frontendImmutableCacheControl
	}
	if strings.HasPrefix(requestPath, "/_nuxt/builds/meta/") {
		return frontendImmutableCacheControl
	}
	return frontendRevalidateCacheControl
}
