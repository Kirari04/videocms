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
	videoData.Get("/:UUID/:QUALITY/:FILE", controllers.GetVideoData)
	videoData.Get("/:UUID/:SUBUUID/subtitle/:FILE", controllers.GetSubtitleData)
	videoData.Get("/:UUID/:SUBUUID/audio/:FILE", controllers.GetAudioData)
}
