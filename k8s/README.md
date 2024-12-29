# Kubernetes Deployment Guide

This guide explains how to deploy the CDN service on Kubernetes.

## Prerequisites

- Kubernetes cluster
- kubectl configured
- Docker registry access

## Components

- **cdn-service**: Main application deployment
- **redis**: Cache service
- **configmap**: Configuration values
- **service**: Load balancer configuration

## Deployment Steps

1. Create namespace (optional):
```bash
kubectl create namespace cdn
kubectl config set-context --current --namespace=cdn
```

2. Apply ConfigMap:
```bash
kubectl apply -f configmap.yaml
```

3. Deploy Redis:
```bash
kubectl apply -f redis.yaml
```

4. Deploy CDN Service:
```bash
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
```

## Verification

Check deployment status:
```bash
kubectl get pods
kubectl get services
```

Check application logs:
```bash
kubectl logs -f deployment/cdn-service
```

## Scaling

Scale the deployment:
```bash
kubectl scale deployment cdn-service --replicas=5
```

## Monitoring

Access metrics at:
- Health check: http://[LOAD_BALANCER_IP]/health
- Metrics: http://[LOAD_BALANCER_IP]/metrics 