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
		Name:  GetEnvOrDefault("APP_NAME", "cdn"),
		Port:  GetEnvAsIntOrDefault("APP_PORT", 9090),
		URL:   GetEnvOrDefault("APP_URL", "http://localhost:9090"),
		Token: GetEnvOrDefault("TOKEN", ""),
	}

	// MinIO Config
	config.Minio = MinioConfig{
		Endpoint: GetEnvOrDefault("MINIO_ENDPOINT", ""),
		User:     GetEnvOrDefault("MINIO_ROOT_USER", ""),
		Password: GetEnvOrDefault("MINIO_ROOT_PASSWORD", ""),
		UseSSL:   GetEnvAsBoolOrDefault("MINIO_USE_SSL", false),
	}

	// AWS Config
	config.AWS = AWSConfig{
		AccessKeyID:     GetEnvOrDefault("AWS_ACCESS_KEY_ID", ""),
		SecretAccessKey: GetEnvOrDefault("AWS_SECRET_ACCESS_KEY", ""),
		SessionToken:    GetEnvOrDefault("AWS_SESSION_TOKEN", ""),
		Region:          GetEnvOrDefault("AWS_REGION", ""),
		Bucket:          GetEnvOrDefault("AWS_BUCKET", ""),
	}

	// Redis Config
	config.Redis = RedisConfig{
		URL: GetEnvOrDefault("REDIS_URL", GetEnvOrDefault("REDIS_URL", "redis://localhost:6379")),
	}

	// Worker Config
	config.Worker = WorkerConfig{
		PoolSize:      GetEnvAsIntOrDefault("WORKER_POOL_SIZE", 5),
		QueueSize:     GetEnvAsIntOrDefault("WORKER_QUEUE_SIZE", 10),
		MaxRetries:    GetEnvAsIntOrDefault("WORKER_MAX_RETRIES", 3),
		RetryDelay:    time.Duration(GetEnvAsIntOrDefault("WORKER_RETRY_DELAY_MS", 1000)) * time.Millisecond,
		BatchSize:     GetEnvAsIntOrDefault("WORKER_BATCH_SIZE", 10),
		FlushTimeout:  time.Duration(GetEnvAsIntOrDefault("WORKER_FLUSH_TIMEOUT_MS", 5000)) * time.Millisecond,
		MaxConcurrent: GetEnvAsIntOrDefault("WORKER_MAX_CONCURRENT", 5),
	}

	// Feature Config
	config.Features = FeatureConfig{
		DisableDelete: GetEnvAsBoolOrDefault("DISABLE_DELETE", false),
		DisableUpload: GetEnvAsBoolOrDefault("DISABLE_UPLOAD", false),
		DisableGet:    GetEnvAsBoolOrDefault("DISABLE_GET", false),
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

func GetEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func GetEnvAsIntOrDefault(key string, defaultValue int) int {
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

func GetEnvAsBoolOrDefault(key string, defaultValue bool) bool {
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
