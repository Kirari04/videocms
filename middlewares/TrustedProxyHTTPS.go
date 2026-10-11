package middlewares

import (
	"net"
	"strings"

	"github.com/labstack/echo/v4"
)

const trustedProxyHTTPSKey = "videocms.trustedProxyHTTPS"

// TrustedProxyHTTPS uses the same networks as the real-IP extractor, but
// checks the immediate connection peer before trusting forwarded protocol.
func TrustedProxyHTTPS(networks []*net.IPNet, trustLocal bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
			ip := net.ParseIP(host)
			trusted := err == nil && ip != nil && trustLocal && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
			if err == nil && ip != nil {
				for _, network := range networks {
					if network != nil && network.Contains(ip) {
						trusted = true
						break
					}
				}
			}
			c.Set(trustedProxyHTTPSKey, trusted && strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https"))
			return next(c)
		}
	}
}

func IsTrustedProxyHTTPS(c echo.Context) bool {
	secure, _ := c.Get(trustedProxyHTTPSKey).(bool)
	return secure
}
