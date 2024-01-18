package middlewares

import (
	"ch/kirari04/videocms/auth"
	"net/http"

	"github.com/labstack/echo/v4"
)

func JwtStream() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			uuid := c.Param("UUID")
			if uuid == "" {
				return c.String(http.StatusBadRequest, "Missing UUID parameter")
			}
			tknStr := c.QueryParam("jwt")
			if tknStr == "" {
				return c.String(http.StatusBadRequest, "UUID parameter match issue")
			}
			token, claims, err := auth.VerifyJWTStream(tknStr)
			if err != nil {
				return c.String(http.StatusForbidden, "Broken JWT")
			}
			if !token.Valid {
				return c.String(http.StatusForbidden, "Invalid JWT")
			}
			if claims.UUID != uuid {
				return c.String(http.StatusForbidden, "Mismacht UUID")
			}
			return next(c)
		}
	})
}
