package helpers

import (
	"ch/kirari04/videocms/config"

	"github.com/gofiber/fiber/v2"
)

var LimiterWhitelistIps = map[string]bool{
	"127.0.0.1": true,
}

func LimiterWhitelistNext(c *fiber.Ctx) bool {
	// disable ratelimit by env
	if config.ENV.RatelimitEnabled == "false" {
		return true
	}
	// disable ratelimit by ip
	if LimiterWhitelistIps[c.IP()] {
		return true
	}

	// ratelimit enabled
	return false
}
