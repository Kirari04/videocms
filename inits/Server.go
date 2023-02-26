package inits

import (
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
	app := fiber.New(fiber.Config{
		Prefork:       false,
		CaseSensitive: true,
		StrictRouting: true,
		ServerHeader:  "Fiber",
		AppName:       os.Getenv("VideoCMS"),
		IdleTimeout:   time.Minute,
		ReadTimeout:   time.Minute * 10,
		WriteTimeout:  time.Minute * 10,
		BodyLimit:     5 * 1024 * 1024 * 1024, //5gb
		Views:         engine,
	})

	// recovering from panics
	app.Use(recover.New())

	// Compression middleware
	app.Use(compress.New(compress.Config{
		Next: func(c *fiber.Ctx) bool {
			return c.Path() == "/videos"
		},
		Level: compress.LevelBestSpeed, // 1
	}))

	// cors configuration
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
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
	listen := os.Getenv("Host")
	if listen == "" {
		listen = "127.0.0.1:3000"
	}

	defer logFile.Close()
	App.Listen(listen)
}
