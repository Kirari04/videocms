package controllers

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/services"
	"ch/kirari04/videocms/services/tusupload"
)

type Handlers struct {
	Deps    *app.Deps
	Auth    *auth.Service
	Logic   *logic.Service
	Workers *services.WorkerGroup
	TUS     *tusupload.Service
}

func NewHandlers(deps *app.Deps, authSvc *auth.Service, logicSvc *logic.Service, workerGroup *services.WorkerGroup, tusSvc *tusupload.Service) *Handlers {
	return &Handlers{
		Deps:    deps,
		Auth:    authSvc,
		Logic:   logicSvc,
		Workers: workerGroup,
		TUS:     tusSvc,
	}
}

func (h *Handlers) Config() config.Config {
	return h.Deps.Config()
}
