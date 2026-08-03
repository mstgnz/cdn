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
TOKEN=your-secret-token        # required; the service refuses to boot if empty

# Minio
MINIO_ENDPOINT=localhost:9000
MINIO_ROOT_USER=your-access-key
MINIO_ROOT_PASSWORD=your-secret-key
MINIO_USE_SSL=false

# AWS (optional)
AWS_ACCESS_KEY_ID=your-aws-access-key
AWS_SECRET_ACCESS_KEY=your-aws-secret-key
AWS_REGION=your-aws-region

# Redis Configuration (credentials/db go inside the URL, e.g. redis://:pass@host:6379/0)
REDIS_URL=redis://localhost:6379

# Rate Limiting
RATE_LIMIT=100            # requests per window
RATE_LIMIT_DURATION=1     # window length in MINUTES (not seconds)
UPLOAD_RATE_LIMIT=50      # stricter limit for upload endpoints
```

See [`.env.example`](../.env.example) for the complete list of variables and
their defaults.

## Test Environment Setup

1. Start the infrastructure the integration tests need (MinIO/Redis). Tests that
   require infrastructure skip cleanly when it is absent:

```bash
docker compose up -d minio redis
```

2. Run the test suite (the build uses cgo + ImageMagick):

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
    branches: [main]
  pull_request:
    branches: [main]

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

3. Run the application (requires cgo + ImageMagick + pkg-config; see the README
   build notes):

```bash
CGO_ENABLED=1 go run ./cmd/main.go
```

## Docker Deployment

The Dockerfile is named `dockerfile` (lowercase), so pass it explicitly when
building directly; Compose already references it.

1. Build the image:

```bash
docker build -f dockerfile -t cdn-api .
```

2. Run with Docker Compose (builds all three API replicas + nginx + MinIO + Redis):

```bash
docker compose up -d --build
```

## Monitoring

The service exposes Prometheus metrics on the app port at `/metrics`
(`http://localhost:${APP_PORT}/metrics`). As of v1.7.0 this endpoint requires a
Bearer token, so configure your Prometheus scrape job with
`authorization.credentials: <TOKEN>`. This repository does not ship a
Prometheus/Grafana Compose stack; point your existing monitoring at the metrics
endpoint.

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

**Do not publish MinIO or Redis on a public interface.** Since v1.10.0
`docker-compose.yml` binds `9000`, `9001` and `6379` to `127.0.0.1`. Nothing in
the stack needs them published: the API replicas reach both over the compose
network. Publishing them exposes an S3 endpoint holding every object, an admin
console, and a password-less Redis. A host firewall does not cover this, because
Docker writes its own iptables rules and a published port bypasses `ufw`. Verify
from *outside* the host:

```bash
nc -zv <public-ip> 9000 9001 6379   # all three should fail to connect
```

**Put the MinIO console behind more than its own login** if you expose it through
a proxy. An `allow`/`deny` block on the office addresses costs nothing and takes
an admin UI off the public internet.

**`TOKEN` must be at least 32 characters** and is checked at boot. Generate one
with `openssl rand -hex 32`. Bucket-scoped tokens in `config/tokens.json` are
held to the same floor.

**In the reverse proxy in front of this service**, do not set
`proxy_ignore_headers Cache-Control`. The service marks a degraded resize
response `no-store`, and ignoring that caches a full-size image under a `?width=`
URL. Be aware too that caching there means a deleted object keeps being served
until its entry expires; the open source nginx build has no purge.

**Uploads are allowlisted by extension, by MIME type and by content signature.**
See [Accepted File Types](./api.md#accepted-file-types). Turn validation off
(`VALIDATE_FILE=false`) only where every caller is trusted.

**Rotate credentials that were ever reachable from outside**, including
`MINIO_ROOT_PASSWORD`, rather than assuming nobody looked.

Then the routine items: TLS termination, keeping the images patched, and
reviewing `docker compose logs api` for the `auth.failure` and
`auth.bucket_access_denied` audit events.

## Backup and Recovery

1. MinIO Backup:

```bash
mc mirror minio/bucket backup/bucket
```

2. Database Backup (if applicable)
3. Configuration Backup

## Troubleshooting

1. Check logs (the app service is `api`; there is no `cdn-service` container):

```bash
docker compose logs -f api
```

2. Monitor metrics (app port, Bearer token required):

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:9090/metrics
```

3. Common issues:

- Connection refused: Check if MinIO is running
- Authentication failed: Verify environment variables
- Rate limit exceeded: Check client IP and adjust limits if needed

## Advanced Deployment Strategies

### Blue/Green Deployment

Blue/Green deployment allows zero-downtime updates by running two identical environments.

1. Initial setup:

```yaml
# blue-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cdn-blue
spec:
  replicas: 3
  selector:
    matchLabels:
      app: cdn
      version: blue
  template:
    metadata:
      labels:
        app: cdn
        version: blue
    spec:
      containers:
        - name: cdn
          image: cdn-service:1.0
          ports:
            - containerPort: 9090
```

2. Service configuration:

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: cdn-service
spec:
  selector:
    app: cdn
    version: blue # Switch between blue/green
  ports:
    - port: 80
      targetPort: 9090
```

3. Deployment process:

```bash
# Deploy new version (green)
kubectl apply -f green-deployment.yaml

# Verify green deployment
kubectl get pods -l version=green

# Switch traffic to green
kubectl patch service cdn-service -p '{"spec":{"selector":{"version":"green"}}}'

# Remove old version (blue)
kubectl delete -f blue-deployment.yaml
```

### Multi-Region Deployment

Configure multiple regions for high availability and lower latency.

1. Regional Kubernetes clusters:

```bash
# Create clusters in different regions
gcloud container clusters create cdn-us-west --region=us-west1
gcloud container clusters create cdn-eu-west --region=eu-west1
gcloud container clusters create cdn-asia-east --region=asia-east1
```

2. Regional configuration:

```yaml
# config-us-west.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cdn-config
data:
  REGION: us-west1
  MINIO_ENDPOINT: minio-us-west.example.com
  REDIS_URL: redis-us-west.example.com
```

3. DNS and Load Balancing:

```yaml
# global-lb.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: cdn-global-ingress
  annotations:
    kubernetes.io/ingress.global-static-ip-name: cdn-global-ip
spec:
  rules:
    - host: cdn.example.com
      http:
        paths:
          - path: /*
            pathType: ImplementationSpecific
            backend:
              service:
                name: cdn-service
                port:
                  number: 80
```

### Disaster Recovery Plan

1. Data Backup Strategy:

```bash
# Automated MinIO backup to secondary storage
mc mirror --watch minio/bucket s3/backup-bucket

# Redis backup
redis-cli SAVE
aws s3 cp dump.rdb s3://backup-bucket/redis/

# Configuration backup
kubectl get all -A -o yaml > k8s-backup.yaml
```

2. Recovery Time Objectives (RTO):

- Critical services: < 1 hour
- Non-critical services: < 4 hours

3. Recovery Point Objectives (RPO):

- Storage data: < 5 minutes
- Cache data: < 1 minute

4. Recovery Steps:

a. Infrastructure Failure:

```bash
# Switch to backup region
kubectl config use-context backup-cluster

# Restore configurations
kubectl apply -f k8s-backup.yaml

# Verify services
kubectl get pods,svc
```

b. Data Corruption:

```bash
# Stop affected services
kubectl scale deployment cdn-service --replicas=0

# Restore from backup
mc mirror s3/backup-bucket minio/bucket

# Restore Redis data
aws s3 cp s3://backup-bucket/redis/dump.rdb .
kubectl cp dump.rdb redis-0:/data/

# Restart services
kubectl scale deployment cdn-service --replicas=3
```

5. Regular Testing:

```bash
# Monthly DR test schedule
0 0 1 * * /scripts/dr-test.sh

# Backup verification
0 0 * * * /scripts/verify-backups.sh
```

### Monitoring and Alerts

1. Regional health checks:

```yaml
# prometheus-rules.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: cdn-alerts
spec:
  groups:
    - name: cdn.rules
      rules:
        - alert: RegionUnhealthy
          expr: cdn_region_health < 1
          for: 5m
          labels:
            severity: critical
          annotations:
            description: "Region {{ $labels.region }} is unhealthy"
```

2. Failover triggers:

```yaml
# failover-policy.yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: cdn-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: cdn
```
