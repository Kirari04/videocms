package routes

import "ch/kirari04/videocms/inits"

func Web() {

	inits.App.Static("/", "./public")
}
