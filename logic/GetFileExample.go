package logic

import (
	"ch/kirari04/videocms/config"
	"net/http"
)

func GetFileExample() (status int, response string, err error) {
	return http.StatusOK, config.ENV.ProjectExampleVideo, nil
}
