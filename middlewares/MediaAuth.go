package middlewares

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/sharing"
	"net/http"

	"github.com/labstack/echo/v4"
)

const MediaClaimsContextKey = "media_claims"

func (f *Factory) MediaAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cookie, err := c.Cookie(auth.MediaCookieName)
			if err != nil || cookie.Value == "" {
				return c.String(http.StatusUnauthorized, "Missing media token")
			}

			token, claims, err := f.authService().VerifyMediaToken(cookie.Value)
			if err != nil || token == nil || claims == nil || !token.Valid {
				return c.String(http.StatusUnauthorized, "Invalid media token")
			}

			uuid := c.Param("UUID")
			if uuid == "" || claims.LinkUUID != uuid {
				return c.String(http.StatusForbidden, "Media token does not match requested video")
			}

			if claims.ShareToken != "" {
				share, err := sharing.Resolve(f.Deps.DB, claims.ShareToken)
				if err != nil || !share.AllowRead || share.AccessVersion != claims.ShareVersion {
					return c.String(http.StatusForbidden, "Shared access has expired or was revoked")
				}
				if err := sharing.ContainsLink(f.Deps.DB, share, uuid); err != nil {
					return c.String(http.StatusForbidden, "Video is outside this share")
				}
				if share.PasswordHash != "" || share.RequireCaptcha {
					cookie, err := c.Cookie(auth.FolderShareCookieName(share.Token))
					if err != nil || !f.authService().VerifyFolderShareToken(cookie.Value, share) {
						return c.String(http.StatusUnauthorized, "Unlock this share first")
					}
				}
			}
			c.Set(MediaClaimsContextKey, claims)
			return next(c)
		}
	}
}

func MediaClaims(c echo.Context) (*auth.MediaClaims, bool) {
	claims, ok := c.Get(MediaClaimsContextKey).(*auth.MediaClaims)
	return claims, ok
}
