package middlewares

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
)

type Factory struct {
	Deps *app.Deps
	Auth *auth.Service
}

func NewFactory(deps *app.Deps, authSvc *auth.Service) *Factory {
	if authSvc == nil {
		authSvc = auth.NewService(deps)
	}
	return &Factory{Deps: deps, Auth: authSvc}
}

func (f *Factory) Config() config.Config {
	if f == nil || f.Deps == nil || f.Deps.Snapshots == nil {
		return config.Config{}
	}
	return f.Deps.Config()
}

func (f *Factory) authService() *auth.Service {
	if f == nil {
		return auth.NewService(nil)
	}
	if f.Auth != nil {
		return f.Auth
	}
	return auth.NewService(f.Deps)
}
