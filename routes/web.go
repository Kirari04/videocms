package routes

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/middlewares"
)

func Web() {
	inits.App.Static("/", "./public")

	inits.App.Get("/:UUID", controllers.PlayerController)

	videoData := inits.App.Group(config.ENV.FolderVideoQualitysPub)
	videoData.Get("/:UUID/stream/muted/master.m3u8", middlewares.JwtStream, controllers.GetM3u8Data)
	videoData.Get("/:UUID/image/thumb/:FILE", middlewares.JwtStream, controllers.GetThumbnailData)
	videoData.Get("/:UUID/:QUALITY/:FILE", middlewares.JwtStream, controllers.GetVideoData)
	videoData.Get("/:UUID/:SUBUUID/subtitle/:FILE", middlewares.JwtStream, controllers.GetSubtitleData)
	videoData.Get("/:UUID/:AUDIOUUID/audio/:FILE", middlewares.JwtStream, controllers.GetAudioData)
	videoData.Get("/:UUID/:AUDIOUUID/stream/master.m3u8", middlewares.JwtStream, controllers.GetM3u8Data)
}
