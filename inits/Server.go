package inits

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

var App *fiber.App
var Api fiber.Router
var logFile *os.File

func Server() {
	app := fiber.New(fiber.Config{
		Prefork:       true,
		CaseSensitive: true,
		StrictRouting: true,
		ServerHeader:  "Fiber",
		AppName:       os.Getenv("VideoCMS"),
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
	log.Fatal(App.Listen(listen))
}
