# Deployment Guide

## Prerequisites
- Go 1.22 or higher
- Docker and Docker Compose
- MinIO Server
- AWS Account (optional)
- Redis Server
- ImageMagick (automatically managed)

## Environment Setup

1. Clone the repository:
```bash
git clone https://github.com/mstgnz/cdn.git
cd cdn
```

2. Copy environment file:
```bash
cp .env.example .env
```

3. Configure environment variables in `.env`:
```env
# App
APP_PORT=9090
APP_TOKEN=your-secret-token

# Minio
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=your-access-key
MINIO_SECRET_KEY=your-secret-key
MINIO_USE_SSL=false

# AWS (optional)
AWS_ACCESS_KEY_ID=your-aws-access-key
AWS_SECRET_ACCESS_KEY=your-aws-secret-key
AWS_REGION=your-aws-region

# Redis Configuration
REDIS_URL=redis://localhost:6379
REDIS_PASSWORD=your-redis-password
REDIS_DB=0

# Rate Limiting
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_DURATION=60
```

## Test Environment Setup

1. Start test environment:
```bash
docker-compose -f docker-compose.test.yml up -d
```

2. Run test suite:
```bash
# All tests
make test

# Unit tests with coverage
make test-coverage

# Load tests
make test-load
```

3. View test results:
```bash
# Coverage report
open coverage.html

# k6 load test report
open k6-report.html
```

## Load Testing

### Scenarios

1. Basic Load Test:
```bash
k6 run test/performance/load_test.js
```

2. Stress Test:
```bash
k6 run --vus 50 --duration 5m test/performance/load_test.js
```

3. Spike Test:
```bash
k6 run --vus 100 --duration 10s test/performance/spike_test.js
```

### Metrics to Monitor
- Request Duration (p95 < 500ms)
- Error Rate (< 1%)
- CPU Usage (< 80%)
- Memory Usage (< 80%)
- Redis Connection Pool
- Storage Operations

## CI/CD Pipeline

### GitHub Actions
```yaml
name: CDN Service CI/CD

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Set up Go
        uses: actions/setup-go@v2
        with:
          go-version: 1.22
      - name: Run Tests
        run: make test
      - name: Upload Coverage
        uses: actions/upload-artifact@v2
        with:
          name: coverage
          path: coverage.html

  build:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - name: Build Docker Image
        run: docker build -t cdn-service .
      - name: Run Load Tests
        run: make test-load
```

## Monitoring Dashboard

### Grafana Dashboard Panels

1. Request Metrics
- Total Requests per Second
- Average Response Time
- Error Rate
- Rate Limit Hits

2. Resource Usage
- CPU Usage
- Memory Usage
- Disk I/O
- Network Traffic

3. Cache Metrics
- Cache Hit Rate
- Cache Size
- Eviction Rate
- Cache Duration

4. Storage Metrics
- Upload Success Rate
- Storage Operations
- Bucket Usage
- File Size Distribution

## Local Development

1. Start MinIO:
```bash
docker-compose up -d minio
```

2. Install dependencies:
```bash
go mod download
```

3. Run the application:
```bash
go run cmd/main.go
```

## Docker Deployment

1. Build the image:
```bash
docker build -t cdn-service .
```

2. Run with Docker Compose:
```bash
docker-compose up -d
```

## Monitoring Setup

1. Start Prometheus and Grafana:
```bash
docker-compose -f docker-compose.monitoring.yml up -d
```

2. Access monitoring:
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3001

## Production Deployment

### Kubernetes

1. Apply Kubernetes manifests:
```bash
kubectl apply -f k8s/
```

2. Configure ingress:
```bash
kubectl apply -f k8s/ingress.yaml
```

### Scaling

- Horizontal scaling:
```bash
kubectl scale deployment cdn-service --replicas=3
```

- Configure resource limits in `k8s/deployment.yaml`:
```yaml
resources:
  limits:
    cpu: "1"
    memory: "1Gi"
  requests:
    cpu: "500m"
    memory: "512Mi"
```

## Security Considerations

1. SSL/TLS Configuration
2. Rate Limiting (already implemented)
3. Authentication
4. Secure Environment Variables
5. Regular Security Updates

## Backup and Recovery

1. MinIO Backup:
```bash
mc mirror minio/bucket backup/bucket
```

2. Database Backup (if applicable)
3. Configuration Backup

## Troubleshooting

1. Check logs:
```bash
docker logs cdn-service
```

2. Monitor metrics:
```bash
curl http://localhost:3000/metrics
```

3. Common issues:
- Connection refused: Check if MinIO is running
- Authentication failed: Verify environment variables
- Rate limit exceeded: Check client IP and adjust limits if needed 