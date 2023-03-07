package inits

import (
	"ch/kirari04/videocms/config"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html"
)

var App *fiber.App
var Api fiber.Router
var logFile *os.File

func Server() {
	engine := html.New("./views", ".html")
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

	app := fiber.New(fiber.Config{
		Prefork:       false,
		CaseSensitive: true,
		StrictRouting: true,
		ServerHeader:  "Fiber",
		AppName:       config.ENV.AppName,
		IdleTimeout:   time.Minute,
		ReadTimeout:   time.Minute * 10,
		WriteTimeout:  time.Minute * 10,
		BodyLimit:     5 * 1024 * 1024 * 1024, //5gb
		Views:         engine,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Printf("Internal server error happend: %v", err)
			return c.SendStatus(fiber.StatusInternalServerError)
		},
		TrustedProxies: trustedProxies,
	})

	// recovering from panics
	app.Use(recover.New(recover.Config{}))

	// Compression middleware
	app.Use(compress.New(compress.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Path() == "/videos"
		},
		Level: compress.LevelBestSpeed, // 1
	}))

	// cors configuration
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowHeaders:     "*",
		AllowCredentials: true,
	}))

	// caches response to be more efficient and save bandwidth
	app.Use(etag.New())

	// Loggin into file
	file, err := os.OpenFile("./logs/access.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Panicf("error opening file: %v", err)
	}
	logFile = file
	// using nginxs logformat: https://docs.nginx.com/nginx-amplify/metrics-metadata/nginx-metrics/#additional-nginx-metrics
	app.Use(logger.New(logger.Config{
		Format: "${ip} - - [${time}] \"${method} ${url} ${protocol}\" " +
			"${status} ${bytesSent} \"${referer}\" " +
			"\"${ua}\" \"${ips}\" " +
			"\"${host}\" sn=\"${port}\" " +
			"rt=${latency} " +
			"ua=\"${route}\" us=\"${status}\" " +
			"ut=\"0\" ul=\"${bytesReceived}\" " +
			"cs=${error}\n",
		Output: file,
	}))

	App = app
	Api = app.Group("/api")
}

func ServerStart() {
	defer logFile.Close()
	App.Listen(config.ENV.Host)
}
