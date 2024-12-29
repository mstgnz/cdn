package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	Minio    MinioConfig
	AWS      AWSConfig
	Redis    RedisConfig
	Worker   WorkerConfig
	Features FeatureConfig
}

type AppConfig struct {
	Name  string
	Port  int
	URL   string
	Token string
}

type MinioConfig struct {
	Endpoint string
	User     string
	Password string
	UseSSL   bool
}

type AWSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
	Bucket          string
}

type RedisConfig struct {
	URL string
}

type WorkerConfig struct {
	PoolSize      int
	QueueSize     int
	MaxRetries    int
	RetryDelay    time.Duration
	BatchSize     int
	FlushTimeout  time.Duration
	MaxConcurrent int
}

type FeatureConfig struct {
	DisableDelete bool
	DisableUpload bool
	DisableGet    bool
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	config := &Config{}

	// App Config
	config.App = AppConfig{
		Name:  getEnvOrDefault("APP_NAME", "cdn"),
		Port:  getEnvAsIntOrDefault("APP_PORT", 9090),
		URL:   getEnvOrDefault("APP_URL", "http://localhost:9090"),
		Token: mustGetEnv("TOKEN"),
	}

	// MinIO Config
	config.Minio = MinioConfig{
		Endpoint: mustGetEnv("MINIO_ENDPOINT"),
		User:     mustGetEnv("MINIO_ROOT_USER"),
		Password: mustGetEnv("MINIO_ROOT_PASSWORD"),
		UseSSL:   getEnvAsBoolOrDefault("MINIO_USE_SSL", false),
	}

	// AWS Config
	config.AWS = AWSConfig{
		AccessKeyID:     mustGetEnv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: mustGetEnv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    getEnvOrDefault("AWS_SESSION_TOKEN", ""),
		Region:          mustGetEnv("AWS_REGION"),
		Bucket:          getEnvOrDefault("AWS_BUCKET", ""),
	}

	// Redis Config
	config.Redis = RedisConfig{
		URL: getEnvOrDefault("REDIS_URL", mustGetEnv("REDIS_URL")),
	}

	// Worker Config
	config.Worker = WorkerConfig{
		PoolSize:      getEnvAsIntOrDefault("WORKER_POOL_SIZE", 5),
		QueueSize:     getEnvAsIntOrDefault("WORKER_QUEUE_SIZE", 10),
		MaxRetries:    getEnvAsIntOrDefault("WORKER_MAX_RETRIES", 3),
		RetryDelay:    time.Duration(getEnvAsIntOrDefault("WORKER_RETRY_DELAY_MS", 1000)) * time.Millisecond,
		BatchSize:     getEnvAsIntOrDefault("WORKER_BATCH_SIZE", 10),
		FlushTimeout:  time.Duration(getEnvAsIntOrDefault("WORKER_FLUSH_TIMEOUT_MS", 5000)) * time.Millisecond,
		MaxConcurrent: getEnvAsIntOrDefault("WORKER_MAX_CONCURRENT", 5),
	}

	// Feature Config
	config.Features = FeatureConfig{
		DisableDelete: getEnvAsBoolOrDefault("DISABLE_DELETE", false),
		DisableUpload: getEnvAsBoolOrDefault("DISABLE_UPLOAD", false),
		DisableGet:    getEnvAsBoolOrDefault("DISABLE_GET", false),
	}

	return config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	var errors []string

	// Validate App Config
	if c.App.Port <= 0 || c.App.Port > 65535 {
		errors = append(errors, "invalid port number")
	}
	if c.App.Token == "" {
		errors = append(errors, "token is required")
	}

	// Validate MinIO Config
	if c.Minio.Endpoint == "" {
		errors = append(errors, "MinIO endpoint is required")
	}
	if c.Minio.User == "" {
		errors = append(errors, "MinIO user is required")
	}
	if c.Minio.Password == "" {
		errors = append(errors, "MinIO password is required")
	}

	// Validate AWS Config
	if c.AWS.AccessKeyID == "" {
		errors = append(errors, "AWS access key ID is required")
	}
	if c.AWS.SecretAccessKey == "" {
		errors = append(errors, "AWS secret access key is required")
	}
	if c.AWS.Region == "" {
		errors = append(errors, "AWS region is required")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(errors, ", "))
	}

	return nil
}

func mustGetEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("environment variable %s is required", key))
	}
	return value
}

func getEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvAsIntOrDefault(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intValue
}

func getEnvAsBoolOrDefault(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return boolValue
}
