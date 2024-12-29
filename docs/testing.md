# Testing Guide

## Prerequisites

The project requires ImageMagick for image processing operations. To ensure a consistent test environment, we recommend running tests inside Docker containers.

## Running Tests

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