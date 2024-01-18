package middlewares

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func IsAdmin() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			isAdmin, ok := c.Get("Admin").(bool)
			if !ok {
				c.Logger().Error("Failed to catch Admin")
				return c.NoContent(http.StatusInternalServerError)
			}
			if !isAdmin {
				return c.String(http.StatusForbidden, "Not Permitted")
			}
			return next(c)
		}
	})
}
