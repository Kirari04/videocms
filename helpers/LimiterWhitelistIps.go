package helpers

import "github.com/gofiber/fiber/v2"

var LimiterWhitelistIps = map[string]bool{
	// "127.0.0.1": true,
}

func LimiterWhitelistNext(c *fiber.Ctx) bool {
	if LimiterWhitelistIps[c.IP()] {
		return true
	}

	return false
}
