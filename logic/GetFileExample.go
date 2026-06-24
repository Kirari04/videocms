package logic

import (
	"net/http"
)

func (s *Service) GetFileExample() (status int, response string, err error) {
	return http.StatusOK, s.Config().ProjectExampleVideo, nil
}
