package inits

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/middlewares"
	"context"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/net/http2"
	"golang.org/x/time/rate"
)

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func fetchCloudflareIPs() []string {
	var ips []string
	client := req.C().SetTimeout(5 * time.Second)
	v4, err4 := client.R().Get("https://www.cloudflare.com/ips-v4")
	v6, err6 := client.R().Get("https://www.cloudflare.com/ips-v6")

	if err4 == nil && v4.IsSuccessState() {
		lines := strings.Split(strings.TrimSpace(v4.String()), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				ips = append(ips, strings.TrimSpace(line))
			}
		}
	}

	if err6 == nil && v6.IsSuccessState() {
		lines := strings.Split(strings.TrimSpace(v6.String()), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				ips = append(ips, strings.TrimSpace(line))
			}
		}
	}

	return ips
}

func fetchBunnyCDNIPs() []string {
	var ips []string
	client := req.C().SetTimeout(5 * time.Second)
	res, err := client.R().Get("https://bunny.net/static/IP_list.txt")

	if err == nil && res.IsSuccessState() {
		lines := strings.Split(strings.TrimSpace(res.String()), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				ips = append(ips, strings.TrimSpace(line))
			}
		}
	}

	return ips
}

func fetchFastlyIPs() []string {
	var ips []string
	client := req.C().SetTimeout(5 * time.Second)
	var data struct {
		Addresses     []string `json:"addresses"`
		Ipv6Addresses []string `json:"ipv6_addresses"`
	}
	res, err := client.R().SetSuccessResult(&data).Get("https://api.fastly.com/public-ip-list")

	if err == nil && res.IsSuccessState() {
		ips = append(ips, data.Addresses...)
		ips = append(ips, data.Ipv6Addresses...)
	}

	return ips
}

func fetchKeyCDNIPs() []string {
	var ips []string
	client := req.C().SetTimeout(5 * time.Second)
	var data struct {
		Data struct {
			Ipv4 []string `json:"ipv4"`
			Ipv6 []string `json:"ipv6"`
		} `json:"data"`
	}
	res, err := client.R().SetSuccessResult(&data).Get("https://www.keycdn.com/ips.json")

	if err == nil && res.IsSuccessState() {
		ips = append(ips, data.Data.Ipv4...)
		ips = append(ips, data.Data.Ipv6...)
	}

	return ips
}

func BuildServer(env config.Config, middlewareFactory *middlewares.Factory) *echo.Echo {
	htmlTemplate := &Template{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}
	trustedProxies := []string{}
	if *env.CloudflareEnabled {
		trustedProxies = append(trustedProxies, fetchCloudflareIPs()...)
	}
	if *env.BunnyCDNEnabled {
		trustedProxies = append(trustedProxies, fetchBunnyCDNIPs()...)
	}
	if *env.FastlyEnabled {
		trustedProxies = append(trustedProxies, fetchFastlyIPs()...)
	}
	if *env.KeyCDNEnabled {
		trustedProxies = append(trustedProxies, fetchKeyCDNIPs()...)
	}
	if env.TrustedProxies != "" {
		manualProxies := strings.Split(env.TrustedProxies, ",")
		for _, proxy := range manualProxies {
			trimmed := strings.TrimSpace(proxy)
			if trimmed != "" {
				trustedProxies = append(trustedProxies, trimmed)
			}
		}
	}
	app := echo.New()
	app.Renderer = htmlTemplate

	// global rate limiter
	app.Use(middleware.RateLimiterWithConfig(*middlewareFactory.LimiterConfig(rate.Limit(env.RatelimitRateGlobal), env.RatelimitBurstGlobal, time.Minute*5)))

	trustLocal := *env.TrustLocalTraffic
	trustOptions := []echo.TrustOption{
		echo.TrustLoopback(trustLocal),   // e.g. ipv4 start with 127.
		echo.TrustLinkLocal(trustLocal),  // e.g. ipv4 start with 169.254
		echo.TrustPrivateNet(trustLocal), // e.g. ipv4 start with 10. or 192.168
	}
	for _, trustedIpRanges := range trustedProxies {
		_, ipNet, err := net.ParseCIDR(trustedIpRanges)
		if err != nil {
			app.Logger.Error("Failed to parse ip range", err)
			continue
		}
		trustOptions = append(trustOptions, echo.TrustIPRange(ipNet))
	}

	app.IPExtractor = echo.ExtractIPFromXFFHeader(trustOptions...)
	app.HTTPErrorHandler = func(errors error, c echo.Context) {
		code := http.StatusInternalServerError
		if he, ok := errors.(*echo.HTTPError); ok {
			code = he.Code
		}

		// in case the route starts with api we respond with json
		if strings.HasPrefix(c.Path(), "/api") {
			c.JSON(code, map[string]string{"error": "Not found"})
			return
		}

		if code == 404 {
			// the backend has 2 types of websites.
			// one is /v/<UUID> containing the player
			// one is env.FolderVideoQualitysPub containing the video data
			// in case its one of thiose and still 404 we render the backend 404 page
			if strings.HasPrefix(c.Path(), env.FolderVideoQualitysPub) || strings.HasPrefix(c.Path(), "/v/") {
				if err := c.Render(code, "404.html", echo.Map{}); err != nil {
					c.Logger().Error(err)
				}
				return
			}

			// now at this point we know its the frontend
			// the frontend is a spa so we can just respond the index.html of the public folder
			if err := c.File("public/index.html"); err != nil {
				c.Logger().Error(err)
			}
		} else {
			c.Logger().Error(errors)
			c.NoContent(code)
		}
	}

	// recovering from panics
	app.Use(middleware.Recover())

	// body limit
	postMaxSize := int64(float64(env.MaxPostSize) / 1024)
	app.Use(middleware.BodyLimitWithConfig(middleware.BodyLimitConfig{
		Limit: fmt.Sprintf("%dk", postMaxSize),
		Skipper: func(c echo.Context) bool {
			// Skip body limit for upload routes, they handle their own limits
			return strings.HasPrefix(c.Path(), "/api/file/upload") || strings.HasPrefix(c.Path(), "/api/uploads")
		},
	}))

	// Compression middleware
	app.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			res := strings.HasPrefix(c.Path(), env.FolderVideoQualitysPub)
			if res {
				c.Response().Header().Add("Compress", "LevelDisabled")
			} else {
				c.Response().Header().Add("Compress", "LevelBestCompression")
			}
			return res
		},
	}))

	// cors configuration
	app.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{env.CorsAllowOrigins},
		AllowMethods:     []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     append([]string{env.CorsAllowHeaders}, tusCorsHeaders()...),
		ExposeHeaders:    tusCorsExposeHeaders(),
		AllowCredentials: *env.CorsAllowCredentials,
		MaxAge:           7200,
	}))

	// Logging
	app.Use(middleware.RequestLogger())

	return app
}

func tusCorsHeaders() []string {
	return []string{
		"Authorization",
		"Content-Type",
		"Tus-Resumable",
		"Upload-Length",
		"Upload-Offset",
		"Upload-Metadata",
		"Upload-Concat",
		"X-HTTP-Method-Override",
	}
}

func tusCorsExposeHeaders() []string {
	return []string{
		"Location",
		"Tus-Resumable",
		"Tus-Version",
		"Tus-Extension",
		"Tus-Max-Size",
		"Upload-Expires",
		"Upload-Length",
		"Upload-Offset",
		"Upload-Metadata",
		"Upload-Concat",
	}
}

func ServerStartFor(app *echo.Echo, host string) {
	// Start server
	go func() {
		if err := app.StartH2CServer(host, &http2.Server{
			MaxConcurrentStreams: uint32(runtime.NumGoroutine()),
			MaxReadFrameSize:     1048576,
			IdleTimeout:          10 * time.Second,
		}); err != nil && err != http.ErrServerClosed {
			app.Logger.Fatal("shutting down the server", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 10 seconds.
	// Use a buffered channel to avoid missing signals as recommended for signal.Notify
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		app.Logger.Fatal(err)
	}
}
