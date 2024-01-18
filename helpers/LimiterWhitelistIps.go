package helpers

import (
	"ch/kirari04/videocms/config"

	"github.com/labstack/echo/v4"
)

var LimiterWhitelistIps = map[string]bool{
	"127.0.0.1": true,
}

func LimiterWhitelistNext(c echo.Context) bool {
	// disable ratelimit by env
	if !*config.ENV.RatelimitEnabled {
		return true
	}
	// disable ratelimit by ip
	if LimiterWhitelistIps[c.RealIP()] {
		return true
	}

	// ratelimit enabled
	return false
}
