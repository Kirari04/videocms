package middlewares

import (
	"ch/kirari04/videocms/models"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

func (f *Factory) AuthMiddleware() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			bearer := c.Request().Header.Get("Authorization")
			if bearer == "" {
				return c.String(http.StatusForbidden, "No JWT Token")
			}
			bearerHeader := strings.Split(bearer, " ")
			tokenString := bearerHeader[len(bearerHeader)-1]

			if strings.HasPrefix(tokenString, "ak_") {
				apiKey, err := f.authService().VerifyApiKey(tokenString)
				if err != nil {
					return c.String(http.StatusForbidden, "Invalid or Expired API Key")
				}

				// Update LastUsedAt and Log Audit (Async for performance)
				go func(akID, uID uint, method, path, ip string) {
					now := time.Now()
					f.Deps.DB.Model(&models.ApiKey{}).Where("id = ?", akID).Update("last_used_at", &now)
					f.Deps.DB.Create(&models.ApiKeyAuditLog{
						ApiKeyID: akID,
						UserID:   uID,
						Method:   method,
						Path:     path,
						IP:       ip,
					})
				}(apiKey.ID, apiKey.UserID, c.Request().Method, c.Request().URL.Path, c.RealIP())

				c.Set("Username", apiKey.User.Username)
				c.Set("UserID", apiKey.UserID)
				c.Set("Admin", apiKey.User.Admin)
				c.Set("IsApiKey", true)
				c.Set("ApiKeyID", apiKey.ID)
				return next(c)
			}

			token, claims, err := f.authService().VerifyJWT(tokenString)
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
	})
}
