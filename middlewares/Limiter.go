package middlewares

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

var LimiterWhitelistIps = map[string]bool{
	"127.0.0.1": true,
}

func (f *Factory) LimiterWhitelistNext(c echo.Context) bool {
	// disable ratelimit by env
	ratelimitEnabled := f.Config().RatelimitEnabled
	if ratelimitEnabled == nil || !*ratelimitEnabled {
		return true
	}
	// disable ratelimit by ip
	if LimiterWhitelistIps[c.RealIP()] {
		return true
	}

	// ratelimit enabled
	return false
}

func (f *Factory) LimiterConfig(rate rate.Limit, burst int, expiration time.Duration) *middleware.RateLimiterConfig {
	return &middleware.RateLimiterConfig{
		Skipper: f.LimiterWhitelistNext,
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{Rate: rate, Burst: burst, ExpiresIn: expiration},
		),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			id := c.RealIP()
			return id, nil
		},
		ErrorHandler: func(c echo.Context, err error) error {
			c.Logger().Error("Ratelimit Error", err)
			return c.NoContent(http.StatusInternalServerError)
		},
		DenyHandler: func(c echo.Context, identifier string, err error) error {
			return c.String(http.StatusTooManyRequests, "Too fast")
		},
	}
}
