package middlewares

import (
	"ch/kirari04/videocms/auth"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func Auth(next, stop echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		bearer := c.Request().Header.Get("Authorization")
		if bearer == "" {
			return c.String(http.StatusForbidden, "No JWT Token")
		}
		bearerHeader := strings.Split(bearer, " ")
		tokenString := bearerHeader[len(bearerHeader)-1]
		token, claims, err := auth.VerifyJWT(tokenString)
		if err != nil {
			return c.String(http.StatusForbidden, "Invalid JWT Token")
		}
		if !token.Valid {
			return c.String(http.StatusForbidden, "Expired JWT Token")
		}
		c.Set("Username", claims.Username)
		c.Set("UserID", claims.UserID)
		c.Set("Admin", claims.Admin)
		return next(c)
	}
}
