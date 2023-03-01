package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/inits"
)

func Web() {

	inits.App.Static("/", "./public")

	inits.App.Get("/:UUID", controllers.PlayerController)

	inits.App.Get("/videos/qualitys/:UUID/:QUALITY/:FILE", controllers.GetVideoData)
}
