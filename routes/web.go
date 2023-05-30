package routes

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
)

func Web() {

	if !*config.ENV.PanelEnabled {
		inits.App.Get("/", controllers.ViewIndex)
	}

	inits.App.Static("/", "./public")

	inits.App.Get("/:UUID", controllers.PlayerController)

	videoData := inits.App.Group("/videos/qualitys")
	videoData.Get("/:UUID/stream/muted/master.m3u8", controllers.GetM3u8Data)
	videoData.Get("/:UUID/image/thumb/:FILE", controllers.GetThumbnailData)
	videoData.Get("/:UUID/:QUALITY/:FILE", controllers.GetVideoData)
	videoData.Get("/:UUID/:SUBUUID/subtitle/:FILE", controllers.GetSubtitleData)
	videoData.Get("/:UUID/:AUDIOUUID/audio/:FILE", controllers.GetAudioData)
	videoData.Get("/:UUID/:AUDIOUUID/stream/master.m3u8", controllers.GetM3u8Data)
}
