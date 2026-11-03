package middlewares

import (
	"strings"

	"github.com/labstack/echo/v4"
)

func SecurityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			path := c.Request().URL.Path
			ancestors := "'none'"
			if strings.HasPrefix(path, "/v/") || strings.HasPrefix(path, "/captcha/") {
				ancestors = "*"
			} else {
				h.Set("X-Frame-Options", "DENY")
			}
			// Inline code is required by the current player templates. Explicit
			// origins constrain third-party scripts while preserving their CDN
			// modules, captcha providers, blob workers and styled subtitles.
			h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; form-action 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdn.plyr.io https://www.google.com/recaptcha/ https://www.gstatic.com/recaptcha/ https://*.hcaptcha.com https://hcaptcha.com https://challenges.cloudflare.com; "+
				"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://cdn.plyr.io https://fonts.googleapis.com; "+
				"font-src 'self' data: https://fonts.gstatic.com; img-src 'self' data: blob: https:; media-src 'self' blob:; worker-src 'self' blob:; "+
				"connect-src 'self' blob: https://api.iconify.design https://cdn.jsdelivr.net https://www.google.com https://www.gstatic.com https://*.hcaptcha.com https://hcaptcha.com https://challenges.cloudflare.com; frame-src 'self' https:; frame-ancestors "+ancestors)
			return next(c)
		}
	}
}
