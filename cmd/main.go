package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/mstgnz/cdn/handler"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/middleware"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/mstgnz/cdn/service"
)

var (
	awsService   service.AwsService
	minioClient  *minio.Client
	imageHandler handler.Image
	awsHandler   handler.AwsHandler
	minioHandler handler.MinioHandler
)

func main() {
	// Logger
	observability.InitLogger()
	logger := observability.Logger()

	// Tracer
	cleanup, initErr := observability.InitTracer("cdn-service", "http://localhost:14268/api/traces")
	if initErr != nil {
		logger.Fatal().Err(initErr).Msg("Failed to initialize tracer")
	}
	defer cleanup()

	err := godotenv.Load(".env")
	if err != nil {
		logger.Fatal().Err(err).Msg("Error loading .env file")
	}

	// watch .env
	go watchEnvChanges()

	awsService = service.NewAwsService()
	minioClient = service.MinioClient()

	// Initialize cache service
	cacheService, err := service.NewCacheService("")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize cache service")
	}

	imageHandler = handler.NewImage(minioClient, awsService)
	awsHandler = handler.NewAwsHandler(awsService)
	minioHandler = handler.NewMinioHandler(minioClient)

	app := fiber.New(fiber.Config{
		BodyLimit: 25 * 1024 * 2014,
	})

	// Global rate limiter - 100 requests per minute
	app.Use(middleware.DefaultRateLimiter())

	// CORS middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
	}))

	app.Use(favicon.New(favicon.Config{
		File: "./public/favicon.png",
	}))

	disableDelete := config.GetEnvAsBoolOrDefault("DISABLE_DELETE", false)
	disableUpload := config.GetEnvAsBoolOrDefault("DISABLE_UPLOAD", false)
	disableGet := config.GetEnvAsBoolOrDefault("DISABLE_GET", false)

	// Swagger
	app.Get("/swagger", func(c *fiber.Ctx) error {
		return c.SendFile("./public/swagger.html")
	})
	app.Get("/swagger.yaml", func(c *fiber.Ctx) error {
		return c.SendFile("./public/swagger.yaml")
	})

	// Health check endpoint
	healthChecker := handler.NewHealthChecker(minioClient, awsService, cacheService)
	app.Get("/health", healthChecker.HealthCheck)

	// Prometheus middleware
	app.Use(observability.PrometheusMiddleware())

	// Metrics endpoint
	app.Get("/metrics", observability.MetricsHandler)

	// Aws
	aws := app.Group("/aws", AuthMiddleware)
	aws.Get("/bucket-list", awsHandler.BucketList)
	aws.Get("/:bucket/exists", awsHandler.BucketExists)
	aws.Get("/vault-list", awsHandler.GlacierVaultList)

	// Minio
	io := app.Group("/minio", AuthMiddleware)
	io.Get("/bucket-list", minioHandler.BucketList)
	io.Get("/:bucket/exists", minioHandler.BucketExists)
	io.Get("/:bucket/create", minioHandler.CreateBucket)
	io.Get("/:bucket/delete", minioHandler.RemoveBucket)

	// resize
	app.Post("/resize", imageHandler.ResizeImage)

	// Minio
	if !disableGet {
		app.Get("/:bucket/w::width/h::height/*", imageHandler.GetImage)
		app.Get("/:bucket/w::width/*", imageHandler.GetImage)
		app.Get("/:bucket/h::height/*", imageHandler.GetImage)
		app.Get("/:bucket/*", imageHandler.GetImage)
	}

	if !disableDelete {
		app.Delete("/:bucket/*", AuthMiddleware, imageHandler.DeleteImage)
	}

	// Upload endpoints with stricter rate limit - 10 requests per minute
	if !disableUpload {
		uploadGroup := app.Group("/")
		uploadGroup.Use(middleware.NewRateLimiter(10, time.Minute))
		uploadGroup.Post("/upload", AuthMiddleware, imageHandler.UploadImage)
		uploadGroup.Post("/upload-url", AuthMiddleware, imageHandler.UploadWithUrl)
	}

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./public/index.html")
	})

	port := fmt.Sprintf(":%s", config.GetEnvOrDefault("APP_PORT", "9090"))
	if err := app.Listen(port); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}

func AuthMiddleware(c *fiber.Ctx) error {
	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid Token", nil)
	}
	return c.Next()
}

// Cross-platform file system notifications for Go.
// Q: Watching a file doesn't work well
// A: Watch the parent directory and use Event.Name to filter out files you're not interested in.
// There is an example of this in cmd/fsnotify/file.go.
func watchEnvChanges() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Close()

	err = watcher.Add("/app")
	if err != nil {
		log.Fatalf("Failed to add .env to watcher: %v", err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if strings.Contains(event.Name, ".env") {
					log.Println("Detected change in .env file, reloading...")
					if err = godotenv.Load(".env"); err != nil {
						log.Println("Load Env Error: ", err)
					}
					if err = service.ReadEnvAndSet(); err != nil {
						log.Println("Read Env Error: ", err)
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Error watching .env file:", err)
		}
	}
}
