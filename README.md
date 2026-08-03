# CDN Service

## About

CDN Service is a high-performance, cloud-native content delivery solution built with Go. It provides a robust and scalable platform for managing and delivering digital assets across multiple cloud providers. The service combines modern architectural patterns with enterprise-grade features to offer:

- **Multi-Cloud Storage**: Seamless integration with MinIO and AWS S3, including Glacier support for cost-effective archival
- **Advanced Image Processing**: Real-time image resizing and optimization with support for various formats
- **Enterprise Features**: Circuit breaker pattern, rate limiting, and batch operations
- **High Performance**: Redis caching, worker pools, and optimized file handling
- **Real-Time Monitoring**: WebSocket-based live monitoring, Prometheus metrics, and comprehensive health checks
- **Developer-Friendly**: Scalar documentation, standardized API responses, and easy deployment options

Perfect for organizations needing a reliable, scalable, and feature-rich content delivery solution with support for multiple cloud providers and advanced monitoring capabilities.

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
- Circuit breaker pattern for fault tolerance
  - Automatic failure detection
  - Graceful service degradation
  - Self-healing capabilities
  - Configurable thresholds and timeouts
  - Real-time state monitoring

### Security

- Token-based authentication
- Bucket-scoped tokens for per-bucket write isolation
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
- Scalar documentation
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

# Optional: path to the bucket-scoped token file (see Authentication below)
TOKENS_FILE=config/tokens.json

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

# ImageMagick resource limits + on-the-fly resize cap
IMAGICK_MEMORY_LIMIT_MB=512
IMAGICK_AREA_LIMIT_MP=256
MAX_RESIZE_DIMENSION=4096

# Opt-in upload optimization (defaults are visually lossless)
OPTIMIZE_MAX_DIMENSION=2560
OPTIMIZE_JPEG_QUALITY=85
OPTIMIZE_WEBP_QUALITY=85
OPTIMIZE_PNG_QUALITY=95

# SSRF guard for /upload-url (true only for self-hosted internal fetches)
UPLOAD_URL_ALLOW_PRIVATE=false
```

> **Breaking change:** `TOKEN` is now **mandatory**. The server refuses to boot
> with an empty `TOKEN`, because an empty server token previously let an empty
> client token bypass authentication on every protected endpoint. Set a
> non-empty `TOKEN` in `.env` before deploying. See the full env reference in
> [`.env.example`](.env.example).

### Authentication

Two kinds of token exist. The general token keeps working exactly as before; the
bucket-scoped tokens are optional and additive.

#### General token

`TOKEN` in `.env`, accepted on every authenticated endpoint.

The service refuses to start unless it is at least 32 characters and is not a
template placeholder (`REPLACE_ME`, `changeme`, `your-token-here` and similar).
A general token is accepted on the operator routes too, so a weak one weakens
every endpoint at once.

```bash
openssl rand -hex 32                          # generate
curl -H "Authorization: Bearer your-token" ...  # use
```

#### Bucket-scoped tokens

A bucket-scoped token authorizes writes to exactly one bucket. Its wire format is
`<bucket>:<token>`:

```bash
curl -X POST http://localhost:9090/upload \
  -H "Authorization: Bearer tedarik:6f2c..." \
  -F "file=@image.jpg"
```

- Accepted on `/upload`, `/batch/upload`, `/upload-url`, `/batch/delete`,
  `DELETE /{bucket}/{path}` and `/resize`.
- The `bucket` form or body field becomes **optional**: the token's own bucket is
  used. Passing a different bucket returns **403**, so one tenant's credential can
  never touch another tenant's objects.
- **Rejected** on the operator endpoints (`/aws/*`, `/minio/*`, `/monitor`,
  `/metrics`, `/ws`). Those act on arbitrary buckets or expose service-wide data
  and need the general token.
- Optionally expiring, via `expires_at` (RFC 3339). Expiry is checked per
  request, so a token stops working the moment it passes without waiting for a
  restart. Omit the field for a token that never expires.

Setup:

```bash
cp config/tokens.template.json config/tokens.json
openssl rand -hex 32          # generate a token per bucket
$EDITOR config/tokens.json
docker compose up -d --build  # the file is read once at boot
```

```json
{
  "buckets": [
    { "bucket": "tedarik", "token": "<32+ characters>", "label": "supply app" },
    { "bucket": "tramer", "token": "<32+ characters>", "expires_at": "2026-12-31T00:00:00Z" }
  ]
}
```

Security notes:

- `config/tokens.json` is git-ignored and docker-ignored. It is mounted read-only
  at runtime and never baked into an image layer.
- Tokens must be at least 32 characters; a bucket name must be 3-63 characters of
  lowercase letters, digits or `-`. An invalid file stops boot rather than
  silently dropping credentials. A missing file is fine and means the general
  token is the only credential.
- Secrets are hashed in memory after load and never appear in logs or responses.
- There is no runtime endpoint that mints or revokes tokens: with three API
  replicas behind nginx such writes would not propagate. Edit the file and
  restart.
- Read access is unaffected. `GET /{bucket}/{path}` stays public, so bucket
  isolation covers writes only.

### API Usage

#### Image Operations

1. Upload an image:

```bash
curl -X POST http://localhost:9090/upload \
  -H "Authorization: Bearer your-token" \
  -F "file=@image.jpg" \
  -F "bucket=your-bucket" \
  -F "path=your/path"
```

With no extra parameter the original bytes are stored unchanged. Add
`optimize=true` to store a visually-lossless, size-reduced version instead
(re-encode + metadata strip + longest-side capped at `OPTIMIZE_MAX_DIMENSION`,
default 2560px). Animated GIFs and non-image files pass through untouched, and
if optimization ever fails the original is stored, so an upload never fails
because of it. Explicit `width`/`height` form values take precedence over the
cap. The same `optimize` flag works on `/batch/upload` (form field) and
`/upload-url` (JSON field).

```bash
curl -X POST http://localhost:9090/upload \
  -H "Authorization: Bearer your-token" \
  -F "file=@photo.jpg" \
  -F "bucket=your-bucket" \
  -F "optimize=true"
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
  -H "Authorization: Bearer your-token"
```

#### Bucket Operations

These require the general token; a bucket-scoped token is rejected.

1. List buckets:

```bash
curl http://localhost:9090/minio/bucket-list \
  -H "Authorization: Bearer your-token"
```

2. Create bucket:

```bash
curl http://localhost:9090/minio/your-bucket/create \
  -H "Authorization: Bearer your-token"
```

### Monitoring

1. Connect to WebSocket for real-time updates:

```javascript
// /ws requires the token as a query parameter (browsers cannot set headers)
const ws = new WebSocket("ws://localhost:9090/ws?token=your-token");
ws.onmessage = (event) => {
  const stats = JSON.parse(event.data);
  console.log("System stats:", stats);
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
curl -H "Authorization: Bearer your-token" http://localhost:9090/monitor
```

## Kubernetes Deployment

For production deployments, we provide comprehensive Kubernetes configurations with:

- Horizontal Pod Autoscaling (3-10 pods)
- Resource quotas and limits
- Health monitoring and readiness probes
- Load balancing strategies
- Secrets management, including the bucket-scoped token file
- Persistent volume claims

For detailed instructions, see [Kubernetes Deployment Guide](k8s/README.md)

## Documentation

For detailed information, please refer to:

- [Cold Storage Archive](docs/archive.md) — keep everything in S3, keep only the recent window on local disk
- [Testing Guide](docs/testing.md)
- [Troubleshooting Guide](docs/troubleshooting.md)
- [Migration Guide](docs/migration.md)
- [Changelog](CHANGELOG.md)
- [Kubernetes Deployment Guide](k8s/README.md)
- [Contributing](CONTRIBUTING.md)

## License

This project is licensed under the Apache License - see the [LICENSE](LICENSE) file for details.
