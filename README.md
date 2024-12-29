# CDN Service

A high-performance Content Delivery Network (CDN) service built with Go, featuring image processing, caching, and multi-cloud storage support.

## Features

### Storage
- Multi-cloud storage support (MinIO, AWS S3)
- Glacier archive support
- Bucket management
- Automatic file type detection
- Secure file handling

### Image Processing
- Real-time image resizing
- Batch processing capabilities
- Worker pool for concurrent operations
- Support for multiple image formats
- URL-based image processing

### Performance
- Redis caching layer with optimized storage
- Batch processing with configurable sizes
- Worker pool for parallel processing
- Rate limiting and request throttling
- Performance metrics and monitoring
- Dynamic ImageMagick version management

### Security
- Token-based authentication
- CORS configuration
- Rate limiting per endpoint with bypass protection
- Redis-based rate limit storage
- Request size limitations
- Trusted proxy support

### Monitoring & Observability
- Prometheus metrics
- Jaeger tracing integration
- Structured logging with zerolog
- Health check endpoints
- Detailed error tracking

### Additional Features
- Environment variable configuration
- Hot reload for configuration changes
- Swagger documentation
- Docker support
- Graceful shutdown

### API Standardization
- Consistent response formats across all endpoints
- Detailed error messages and codes
- Standardized success/error patterns
- Comprehensive request validation

## Quick Start

### Prerequisites
- Go 1.22+
- Docker and Docker Compose
- MinIO Server (or AWS S3 access)
- Redis Server

### Installation

1. Clone the repository:
```bash
git clone https://github.com/mstgnz/cdn.git
cd cdn
```

2. Copy the example environment file:
```bash
cp .env.example .env
```

3. Start the services using Docker Compose:
```bash
docker-compose up -d
```

### Configuration

Edit the `.env` file with your settings:

```env
APP_PORT=9090
APP_NAME=cdn
TOKEN=your-secure-token

# MinIO Configuration
MINIO_ENDPOINT=localhost:9000
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minioadmin
MINIO_USE_SSL=false

# AWS Configuration (optional)
AWS_ACCESS_KEY_ID=your-access-key
AWS_SECRET_ACCESS_KEY=your-secret-key
AWS_REGION=your-region

# Redis Configuration
REDIS_URL=redis://localhost:6379

# Feature Flags
DISABLE_DELETE=false
DISABLE_UPLOAD=false
DISABLE_GET=false
```

### API Usage

#### Image Operations

1. Upload an image:
```bash
curl -X POST http://localhost:9090/upload \
  -H "Authorization: your-token" \
  -F "file=@image.jpg" \
  -F "bucket=your-bucket" \
  -F "path=your/path"
```

2. Get an image with resizing:
```bash
# Original size
http://localhost:9090/your-bucket/image.jpg

# Resize with width
http://localhost:9090/your-bucket/w:300/image.jpg

# Resize with height
http://localhost:9090/your-bucket/h:200/image.jpg

# Resize with both
http://localhost:9090/your-bucket/w:300/h:200/image.jpg
```

3. Delete an image:
```bash
curl -X DELETE http://localhost:9090/your-bucket/image.jpg \
  -H "Authorization: your-token"
```

#### Bucket Operations

1. List buckets:
```bash
curl http://localhost:9090/minio/bucket-list \
  -H "Authorization: your-token"
```

2. Create bucket:
```bash
curl http://localhost:9090/minio/your-bucket/create \
  -H "Authorization: your-token"
```

### Monitoring

- Metrics: `http://localhost:9090/metrics`
- Health Check: `http://localhost:9090/health`
- Swagger Documentation: `http://localhost:9090/swagger`

## Kubernetes Deployment

For production deployments, we provide comprehensive Kubernetes configurations with:
- Horizontal Pod Autoscaling (3-10 pods)
- Resource quotas and limits
- Health monitoring and readiness probes
- Load balancing strategies
- Secrets management
- Persistent volume claims

For detailed instructions, see [Kubernetes Deployment Guide](k8s/README.md)

## Testing

### Prerequisites for Testing
The project requires ImageMagick for image processing operations. To ensure a consistent test environment, we recommend running tests inside Docker containers.

### Running Tests

1. Build and start the containers:
```bash
docker-compose up -d
```

2. Run tests inside the container:
```bash
# Run all tests
docker exec cdn-golang go test ./... -v

# Run specific package tests
docker exec cdn-golang go test ./pkg/worker -v
docker exec cdn-golang go test ./service -v
docker exec cdn-golang go test ./handler -v
```

3. Run tests with coverage:
```bash
docker exec cdn-golang go test ./... -coverprofile=coverage.out
docker exec cdn-golang go tool cover -html=coverage.out -o coverage.html
```

### Test Environment
The test container includes:
- ImageMagick (latest version, dynamically managed)
- Redis for caching and rate limiting tests
- MinIO for storage tests
- Mock AWS services
- k6 for load testing

### Test Coverage
- Unit tests with minimum 80% coverage
- Integration tests for all endpoints
- Performance tests using k6
- Load testing scenarios
- Automated API testing

### Load Testing
```bash
# Run basic load test
k6 run test/performance/load_test.js

# Run stress test
k6 run --vus 50 --duration 5m test/performance/load_test.js

# Run spike test
k6 run --vus 100 --duration 10s test/performance/spike_test.js
```

## Architecture

The service is built with a modular architecture:

- `cmd/`: Application entry point
- `handler/`: Request handlers
- `service/`: Core business logic
- `pkg/`:
  - `batch/`: Batch processing
  - `worker/`: Worker pool
  - `middleware/`: HTTP middlewares
  - `observability/`: Monitoring and tracing
  - `config/`: Configuration management

## Performance Optimizations

- Redis caching for resized images
- Worker pool for concurrent image processing
- Batch processing for bulk operations
- Rate limiting with bypass protection
- Efficient memory management
- Dynamic resource allocation

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a new Pull Request

## License

This project is licensed under the Apache License - see the [LICENSE](LICENSE) file for details.