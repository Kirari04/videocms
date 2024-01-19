package inits

import (
	"ch/kirari04/videocms/config"
	"context"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var App *echo.Echo
var Api echo.Group

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func Server() {
	htmlTemplate := &Template{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}
	trustedProxies := []string{}
	if *config.ENV.CloudflareEnabled {
		trustedProxies = append(trustedProxies, []string{
			"173.245.48.0/20",
			"103.21.244.0/22",
			"103.22.200.0/22",
			"103.31.4.0/22",
			"141.101.64.0/18",
			"108.162.192.0/18",
			"190.93.240.0/20",
			"188.114.96.0/20",
			"197.234.240.0/22",
			"198.41.128.0/17",
			"162.158.0.0/15",
			"104.16.0.0/13",
			"104.24.0.0/14",
			"172.64.0.0/13",
			"131.0.72.0/22",
			"2400:cb00::/32",
			"2606:4700::/32",
			"2803:f800::/32",
			"2405:b500::/32",
			"2405:8100::/32",
			"2a06:98c0::/29",
			"2c0f:f248::/32",
		}...)
	}
	app := echo.New()
	app.Renderer = htmlTemplate
	trustOptions := []echo.TrustOption{
		echo.TrustLoopback(false),   // e.g. ipv4 start with 127.
		echo.TrustLinkLocal(false),  // e.g. ipv4 start with 169.254
		echo.TrustPrivateNet(false), // e.g. ipv4 start with 10. or 192.168
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
	app.HTTPErrorHandler = func(err error, c echo.Context) {
		code := http.StatusInternalServerError
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		c.Logger().Error(err)
		if code == 404 {
			if err := c.Render(code, "404.html", echo.Map{}); err != nil {
				c.Logger().Error(err)
			}
		} else {
			c.NoContent(code)
		}
	}

	// recovering from panics
	app.Use(middleware.Recover())

	// body limit
	e := echo.New()
	postMaxSize := int64(float64(config.ENV.MaxPostSize) / 1000)
	e.Use(middleware.BodyLimit(fmt.Sprintf("%dk", postMaxSize)))

	// Compression middleware
	app.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			res := strings.HasPrefix(c.Path(), config.ENV.FolderVideoQualitysPub)
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
		AllowOrigins:     []string{config.ENV.CorsAllowOrigins},
		AllowHeaders:     []string{config.ENV.CorsAllowHeaders},
		AllowCredentials: *config.ENV.CorsAllowCredentials,
	}))

	// Logging
	app.Use(middleware.Logger())

	App = app
	Api = *app.Group("/api")
}

func ServerStart() {
	// Start server
	go func() {
		if err := App.Start(config.ENV.Host); err != nil && err != http.ErrServerClosed {
			App.Logger.Fatal("shutting down the server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 10 seconds.
	// Use a buffered channel to avoid missing signals as recommended for signal.Notify
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := App.Shutdown(ctx); err != nil {
		App.Logger.Fatal(err)
	}
}
