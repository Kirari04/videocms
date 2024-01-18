package routes

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/middlewares"
)

func Web() {
	inits.App.Static("/", "./public")

	inits.App.GET("/:UUID", controllers.PlayerController)

	videoData := inits.App.Group(config.ENV.FolderVideoQualitysPub)
	videoData.GET("/:UUID/stream/muted/master.m3u8", controllers.GetM3u8Data, middlewares.JwtStream())
	videoData.GET("/:UUID/image/thumb/:FILE", controllers.GetThumbnailData, middlewares.JwtStream())
	videoData.GET("/:UUID/:SUBUUID/subtitle/:FILE", controllers.GetSubtitleData, middlewares.JwtStream())
	videoData.GET("/:UUID/:AUDIOUUID/stream/master.m3u8", controllers.GetM3u8Data, middlewares.JwtStream())
	videoData.GET("/:UUID/:QUALITY/download/video.mkv", controllers.DownloadVideoController, middlewares.JwtStream())
	// no jwt stream
	videoData.GET("/:UUID/:QUALITY/:FILE", controllers.GetVideoData)
	videoData.GET("/:UUID/:AUDIOUUID/audio/:FILE", controllers.GetAudioData)
}
