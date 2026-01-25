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
	"runtime"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/net/http2"
)

var App *echo.Echo
var Api echo.Group

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

func Server() {
	htmlTemplate := &Template{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}
	trustedProxies := []string{}
	if *config.ENV.CloudflareEnabled {
		trustedProxies = append(trustedProxies, fetchCloudflareIPs()...)
	}
	if *config.ENV.BunnyCDNEnabled {
		trustedProxies = append(trustedProxies, fetchBunnyCDNIPs()...)
	}
	if *config.ENV.FastlyEnabled {
		trustedProxies = append(trustedProxies, fetchFastlyIPs()...)
	}
	if *config.ENV.KeyCDNEnabled {
		trustedProxies = append(trustedProxies, fetchKeyCDNIPs()...)
	}
	if config.ENV.TrustedProxies != "" {
		manualProxies := strings.Split(config.ENV.TrustedProxies, ",")
		for _, proxy := range manualProxies {
			trimmed := strings.TrimSpace(proxy)
			if trimmed != "" {
				trustedProxies = append(trustedProxies, trimmed)
			}
		}
	}
	app := echo.New()
	app.Renderer = htmlTemplate
	trustLocal := *config.ENV.TrustLocalTraffic
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

		if code == 404 {
			if err := c.Render(code, "404.html", echo.Map{}); err != nil {
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
		MaxAge:           7200,
	}))

	// Logging
	app.Use(middleware.RequestLogger())

	App = app
	Api = *app.Group("/api")
}

func ServerStart() {
	// Start server
	go func() {
		if err := App.StartH2CServer(config.ENV.Host, &http2.Server{
			MaxConcurrentStreams: uint32(runtime.NumGoroutine()),
			MaxReadFrameSize:     1048576,
			IdleTimeout:          10 * time.Second,
		}); err != nil && err != http.ErrServerClosed {
			App.Logger.Fatal("shutting down the server", err)
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
