package helpers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func LimiterConfig(max int, expiration time.Duration) *limiter.Config {
	return &limiter.Config{
		Next:       LimiterWhitelistNext,
		Max:        max,
		Expiration: expiration,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).SendString("Too fast")
		},
	}
}
