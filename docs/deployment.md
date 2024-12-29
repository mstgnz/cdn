# Deployment Guide

## Prerequisites
- Go 1.22 or higher
- Docker and Docker Compose
- MinIO Server
- AWS Account (optional)
- ImageMagick

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
APP_PORT=3000
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
```

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