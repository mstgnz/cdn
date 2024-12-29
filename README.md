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
- Real-time system monitoring via WebSocket
- Live performance metrics
  - Active uploads count
  - Upload speed
  - Cache hit rate
  - CPU usage
  - Memory usage
  - Disk usage by mount point
  - Recent error logs

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

1. Connect to WebSocket for real-time updates:
```javascript
const ws = new WebSocket('ws://localhost:9090/ws');
ws.onmessage = (event) => {
    const stats = JSON.parse(event.data);
    console.log('System stats:', stats);
    // Example stats data:
    // {
    //   "timestamp": "2024-01-15T10:30:00Z",
    //   "active_uploads": 5,
    //   "upload_speed": 1048576, // bytes/sec
    //   "cache_hit_rate": 85.5,  // percentage
    //   "cpu_usage": 45.2,       // percentage
    //   "memory_usage": 60.8,    // percentage
    //   "disk_usage": {
    //     "/data": 75,           // percentage
    //     "/uploads": 45
    //   },
    //   "errors": [
    //     "Failed to process image: invalid format"
    //   ]
    // }
};
```

2. Get current monitoring stats:
```bash
curl -H "Authorization: your-token" http://localhost:9090/monitor
```

## Kubernetes Deployment

For production deployments, we provide comprehensive Kubernetes configurations with:
- Horizontal Pod Autoscaling (3-10 pods)
- Resource quotas and limits
- Health monitoring and readiness probes
- Load balancing strategies
- Secrets management
- Persistent volume claims

For detailed instructions, see [Kubernetes Deployment Guide](k8s/README.md)

## Documentation

For detailed information, please refer to:

- [Testing Guide](docs/testing.md)
- [Troubleshooting Guide](docs/troubleshooting.md)
- [Migration Guide](docs/migration.md)
- [Changelog](CHANGELOG.md)
- [Kubernetes Deployment Guide](k8s/README.md)
- [Contributing](CONTRIBUTING.md)

## License

This project is licensed under the Apache License - see the [LICENSE](LICENSE) file for details.