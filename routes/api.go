package routes

import (
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func Api() {
	inits.Api.Get("", func(c *fiber.Ctx) error {
		return c.SendString("Salamn")
	})

	inits.Api.
		Use(limiter.New(*helpers.LimiterConfig(10, time.Hour))).
		Post("/login", controllers.Login)
}
