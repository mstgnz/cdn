# Testing Guide

## Prerequisites

The project uses cgo bindings to ImageMagick, so `go test` needs cgo +
ImageMagick + pkg-config. Tests that need infrastructure (MinIO/Redis/AWS) skip
cleanly when it is absent; start MinIO/Redis first to exercise them:

```bash
docker compose up -d minio redis
```

On macOS the build needs these environment variables (adjust paths to your
install):

```bash
export PKG_CONFIG_PATH="/opt/homebrew/opt/imagemagick/lib/pkgconfig"
export CGO_ENABLED=1
export CGO_CFLAGS_ALLOW='-Xpreprocessor'
```

## Running Tests

```bash
# Run all tests
go test ./... -v

# Run specific package tests
go test ./pkg/worker -v
go test ./service -v
go test ./handler -v

# Race detector (recommended for the concurrent paths)
go test -race ./handler/... ./service/...
```

Run with coverage:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

## Test Environment

The test container includes:

- ImageMagick (latest version, dynamically managed)
- Redis for caching and rate limiting tests
- MinIO for storage tests
- Mock AWS services
- k6 for load testing

## Test Coverage

- Unit tests with minimum 80% coverage
- Integration tests for all endpoints
- Performance tests using k6
- Load testing scenarios
- Automated API testing

## Load Testing

```bash
# Run basic load test
k6 run test/performance/load_test.js

# Run stress test
k6 run --vus 50 --duration 5m test/performance/load_test.js

# Run spike test
k6 run --vus 100 --duration 10s test/performance/spike_test.js
```
