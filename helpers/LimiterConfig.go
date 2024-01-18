package helpers

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

func LimiterConfig(rate rate.Limit, burst int, expiration time.Duration) *middleware.RateLimiterConfig {
	return &middleware.RateLimiterConfig{
		Skipper: LimiterWhitelistNext,
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
