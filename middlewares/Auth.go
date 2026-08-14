package middlewares

import (
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
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

				// Persist the audit intent before executing the API-key request. This
				// replaces the former detached best-effort goroutine.
				if f.Deps.Background != nil {
					ownerID := apiKey.UserID
					_, _, enqueueErr := f.Deps.Background.Enqueue(c.Request().Context(), background.JobSpec{
						Kind: "audit.record", Visibility: background.VisibilitySystem, OwnerID: &ownerID,
						SubjectType: "api_key", SubjectID: fmt.Sprint(apiKey.ID),
						IdempotencyKey: "audit:" + uuid.NewString(), Label: "Record API-key activity",
						Tasks: []background.TaskSpec{{
							Kind: "audit.record", Queue: background.QueueAudit, Phase: "Recording API activity",
							Payload:   map[string]any{"apiKeyId": apiKey.ID, "userId": apiKey.UserID, "method": c.Request().Method, "path": c.Request().URL.Path, "ip": c.RealIP()},
							DedupeKey: "record", Priority: 10, Required: true, Weight: 1,
						}},
					})
					if enqueueErr != nil {
						return c.String(http.StatusServiceUnavailable, "API audit service unavailable")
					}
				} else {
					now := time.Now()
					f.Deps.DB.Model(&models.ApiKey{}).Where("id = ?", apiKey.ID).Update("last_used_at", &now)
					f.Deps.DB.Create(&models.ApiKeyAuditLog{
						ApiKeyID: apiKey.ID,
						UserID:   apiKey.UserID,
						Method:   c.Request().Method,
						Path:     c.Request().URL.Path,
						IP:       c.RealIP(),
					})
				}

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
