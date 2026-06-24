package auth

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
)

type Service struct {
	Deps *app.Deps
}

func NewService(deps *app.Deps) *Service {
	return &Service{Deps: deps}
}

func (s *Service) Config() config.Config {
	if s == nil || s.Deps == nil || s.Deps.Snapshots == nil {
		return config.Config{}
	}
	return s.Deps.Config()
}
