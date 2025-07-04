package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/gofiber/websocket/v2"
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
	wsHandler    handler.WebSocketHandler
)

func main() {
	// Logger
	observability.InitLogger()
	logger := observability.Logger()

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	envWatcher := make(chan bool)
	go watchEnvChanges(ctx, envWatcher)

	awsService = service.NewAwsService()
	minioClient = service.MinioClient()
	imageService := &service.ImageService{
		MinioClient: minioClient,
	}
	statsService := service.NewStatsService()

	// Initialize cache service
	cacheService, err := service.NewCacheService()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to initialize cache service, continuing without cache")
		cacheService = nil
	}

	// Initialize handlers
	imageHandler = handler.NewImage(minioClient, awsService, imageService)
	awsHandler = handler.NewAwsHandler(awsService)
	minioHandler = handler.NewMinioHandler(minioClient)
	wsHandler = handler.NewWebSocketHandler(statsService)

	app := fiber.New(fiber.Config{
		BodyLimit: 100 * 1024 * 1024, // 100MB to match nginx configuration
		// Enable graceful shutdown
		DisableStartupMessage: true,
		IdleTimeout:           60 * time.Second,
		ReadTimeout:           60 * time.Second,
		WriteTimeout:          60 * time.Second,
		ReadBufferSize:        24 * 1024 * 1024, // 24MB header buffer size
	})

	// Global rate limiter - 100 requests per minute with IP + Token based protection
	app.Use(middleware.DefaultAdvancedRateLimiter())

	// CORS middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
		AllowMethods: "*",
		MaxAge:       86400,
	}))

	app.Use(favicon.New(favicon.Config{
		File: "./public/favicon.png",
	}))

	disableDelete := config.GetEnvAsBoolOrDefault("DISABLE_DELETE", false)
	disableUpload := config.GetEnvAsBoolOrDefault("DISABLE_UPLOAD", false)
	disableGet := config.GetEnvAsBoolOrDefault("DISABLE_GET", false)

	// scalar
	app.Get("/scalar.yaml", func(c *fiber.Ctx) error {
		// Read the scalar file
		scalarContent, err := os.ReadFile("./public/scalar.yaml")
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": "Failed to read scalar file",
			})
		}

		// Replace environment variables
		scalarContent = []byte(strings.ReplaceAll(string(scalarContent), "${APP_URL}", config.GetEnvOrDefault("APP_URL", "https://cdn.example.com")))

		// Set content type and send the modified content
		c.Set("Content-Type", "text/yaml")
		return c.Send(scalarContent)
	})

	// Health check endpoint
	healthChecker := handler.NewHealthChecker(minioClient, awsService, cacheService)
	app.Get("/health", healthChecker.HealthCheck)

	// Prometheus middleware
	app.Use(observability.PrometheusMiddleware())

	// Metrics endpoint
	app.Get("/metrics", observability.MetricsHandler)

	// WebSocket middleware
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	// WebSocket endpoint
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		wsHandler.HandleWebSocket(c)
	}))

	// Monitoring endpoint
	app.Get("/monitor", AuthMiddleware, wsHandler.MonitorStats)

	// Aws
	aws := app.Group("/aws", AuthMiddleware)
	aws.Get("/bucket-list", awsHandler.BucketList)
	aws.Get("/:bucket/exists", awsHandler.BucketExists)
	aws.Get("/vault-list", awsHandler.GlacierVaultList)

	// Glacier endpoints
	aws.Post("/glacier/:vault/initiate-retrieval/:archiveId", awsHandler.GlacierInitiateRetrieval)
	aws.Get("/glacier/:vault/jobs", awsHandler.GlacierListJobs)
	aws.Get("/glacier/:vault/jobs/:jobId/status", awsHandler.GlacierJobStatus)
	aws.Get("/glacier/:vault/jobs/:jobId/download", awsHandler.GlacierDownloadArchive)
	aws.Post("/glacier/:vault/inventory", awsHandler.GlacierInventoryRetrieval)

	// Async download endpoints
	aws.Post("/glacier/:vault/jobs/:jobId/async-download", awsHandler.GlacierInitiateAsyncDownload)
	aws.Get("/glacier/downloads/:downloadJobId/status", awsHandler.GlacierCheckDownloadStatus)

	// Minio
	io := app.Group("/minio", AuthMiddleware)
	io.Get("/bucket-list", minioHandler.BucketList)
	io.Get("/:bucket/exists", minioHandler.BucketExists)
	io.Get("/:bucket/create", minioHandler.CreateBucket)
	io.Delete("/:bucket/delete", minioHandler.RemoveBucket)

	// resize
	app.Post("/resize", imageHandler.ResizeImage)

	// Minio
	if !disableGet {
		/*
			- The width and height parameters use the w and h prefix to avoid conflicts with numeric values in file paths.
			- Example: a file path like `photos/2024/01/30/image.jpg` can be misinterpreted as resizing parameters.

			- The query parameters are used to resize the image.
			- Example: `https://cdn.example.com/photos/2024/01/30/image.jpg?width=100&height=100`
		*/
		app.Get("/:bucket/w::width/h::height/*", imageHandler.GetImage)
		app.Get("/:bucket/w::width/*", imageHandler.GetImage)
		app.Get("/:bucket/h::height/*", imageHandler.GetImage)
		app.Get("/:bucket/*", imageHandler.GetImage)
	}

	if !disableDelete {
		app.Delete("/:bucket/*", AuthMiddleware, imageHandler.DeleteImage)
	}

	// Upload endpoints with stricter rate limit - 50 requests per minute
	if !disableUpload {
		uploadGroup := app.Group("/")
		uploadGroup.Use(middleware.NewAdvancedRateLimiter(config.GetEnvAsIntOrDefault("UPLOAD_RATE_LIMIT", 50), time.Minute))
		uploadGroup.Post("/upload", AuthMiddleware, imageHandler.UploadImage)
		uploadGroup.Post("/upload-url", AuthMiddleware, imageHandler.UploadWithUrl)
		uploadGroup.Post("/batch/upload", AuthMiddleware, imageHandler.BatchUpload)
		uploadGroup.Delete("/batch/delete", AuthMiddleware, imageHandler.BatchDelete)
	}

	// Index
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./public/scalar.html")
	})

	// Graceful shutdown setup
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		port := fmt.Sprintf(":%s", config.GetEnvOrDefault("APP_PORT", "9090"))
		if err := app.Listen(port); err != nil {
			if err.Error() != "server closed" {
				logger.Fatal().Err(err).Msg("Failed to start server")
			}
		}
	}()

	logger.Info().Msg("Server started successfully")

	// Wait for shutdown signal
	<-shutdownChan
	logger.Info().Msg("Shutting down server...")

	// Cancel context to stop background tasks
	cancel()

	// Stop env watcher
	envWatcher <- true

	// Shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Perform cleanup
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Server shutdown failed")
	}

	// Close other connections
	if err := cacheService.Close(); err != nil {
		logger.Error().Err(err).Msg("Cache service shutdown failed")
	}

	logger.Info().Msg("Server gracefully stopped")
}

func AuthMiddleware(c *fiber.Ctx) error {
	if err := service.CheckToken(c); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}
	return c.Next()
}

// watchEnvChanges monitors .env file changes with context support
func watchEnvChanges(ctx context.Context, done chan bool) {
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
		case <-ctx.Done():
			return
		case <-done:
			return
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
