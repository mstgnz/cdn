# Kubernetes Deployment Guide

This directory contains Kubernetes manifests for deploying the CDN service in a scalable and resilient manner.

## Components

### 1. Deployment (`deployment.yaml`)
- Base configuration for the CDN service
- Resource limits and requests
- Health checks (readiness and liveness probes)
- Environment variable configuration

```yaml
resources:
  limits:
    cpu: "1"
    memory: "1Gi"
  requests:
    cpu: "200m"
    memory: "512Mi"
```

### 2. Service (`service.yaml`)
- LoadBalancer type service
- Exposes port 80 externally
- Routes traffic to pods on port 3000

### 3. Horizontal Pod Autoscaler (`hpa.yaml`)
- Automatic scaling based on CPU and Memory usage
- Scale configuration:
  - Minimum replicas: 3
  - Maximum replicas: 10
  - CPU target utilization: 70%
  - Memory target utilization: 80%
- Scale behavior:
  - Scale up: 2 pods every 60 seconds
  - Scale down: 1 pod every 60 seconds with 300s stabilization

### 4. ConfigMap (`configmap.yaml`)
- Non-sensitive configuration
- MinIO endpoint configuration
- Feature flags

### 5. Secrets (`secrets.yaml`)
- Sensitive information storage
- API tokens and keys
- Must be base64 encoded

## Deployment Instructions

1. **Prepare Environment**
```bash
# Create namespace (optional)
kubectl create namespace cdn

# Create secrets (replace placeholders with actual values)
echo -n 'your-token' | base64
kubectl create -f secrets.yaml

# Apply ConfigMap
kubectl apply -f configmap.yaml
```

2. **Deploy Application**
```bash
# Apply all manifests
kubectl apply -f .

# Or apply individually
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
kubectl apply -f hpa.yaml
```

3. **Verify Deployment**
```bash
# Check deployment status
kubectl get deployments
kubectl get pods

# Check service
kubectl get services

# Check HPA
kubectl get hpa
kubectl describe hpa cdn-service-hpa
```

## Monitoring and Scaling

### Monitor Resources
```bash
# Watch pod scaling
kubectl get pods -w

# Check pod metrics
kubectl top pods

# View HPA status
kubectl get hpa cdn-service-hpa -w
```

### Manual Scaling
```bash
# Scale deployment manually if needed
kubectl scale deployment cdn-service --replicas=5
```

### Logs and Debugging
```bash
# View pod logs
kubectl logs -f deployment/cdn-service

# Check events
kubectl get events --sort-by='.lastTimestamp'
```

## Configuration Updates

### Update ConfigMap
```bash
kubectl edit configmap cdn-config
# or
kubectl apply -f configmap.yaml
```

### Update Secrets
```bash
kubectl create secret generic cdn-secrets \
  --from-literal=app_token=new-token \
  --from-literal=minio_access_key=new-key \
  -n default \
  --dry-run=client -o yaml | kubectl apply -f -
```

## Best Practices

1. **Resource Management**
   - Always set resource requests and limits
   - Monitor resource usage and adjust as needed
   - Use HPA for automatic scaling

2. **Security**
   - Keep secrets in Kubernetes Secrets
   - Regularly rotate credentials
   - Use namespaces for isolation

3. **High Availability**
   - Maintain minimum 3 replicas
   - Use pod anti-affinity for better distribution
   - Configure proper health checks

4. **Monitoring**
   - Monitor HPA metrics
   - Watch for pod restarts
   - Check resource utilization

## Troubleshooting

### Common Issues

1. **Pods Not Starting**
```bash
kubectl describe pod <pod-name>
kubectl logs <pod-name>
```

2. **HPA Not Scaling**
```bash
kubectl describe hpa cdn-service-hpa
kubectl get --raw "/apis/metrics.k8s.io/v1beta1/namespaces/default/pods"
```

3. **Service Not Accessible**
```bash
kubectl get svc
kubectl describe svc cdn-service
```

### Health Checks
- Access health endpoint: `http://<service-ip>/health`
- Check readiness/liveness probe results in pod events 

## Load Balancing Strategy

Our CDN service implements a comprehensive load balancing strategy using multiple layers:

### 1. Network Load Balancer (NLB)
- Layer 4 load balancing
- Cross-zone load balancing enabled
- Connection draining for graceful pod termination
- TCP health checks

### 2. Ingress Controller (NGINX)
- Layer 7 load balancing
- Path-based routing
- SSL/TLS termination
- Advanced caching configuration:
  - 10GB cache size
  - 60-minute cache validity
  - Stale cache usage during backend errors

### 3. Session Affinity
- Client IP-based session stickiness
- 3-hour session timeout
- Ensures consistent user experience

### 4. Load Balancing Configuration

1. **Service Configuration**
```yaml
annotations:
  service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
  service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
sessionAffinity: ClientIP
sessionAffinityConfig:
  clientIP:
    timeoutSeconds: 10800  # 3 hours
```

2. **Ingress Configuration**
```yaml
annotations:
  nginx.ingress.kubernetes.io/proxy-body-size: "25m"
  nginx.ingress.kubernetes.io/proxy-next-upstream-tries: "3"
```

### 5. Caching Strategy
- In-memory caching for frequently accessed content
- Nginx proxy caching:
  - Cache successful responses for 60 minutes
  - Use stale cache during backend errors
  - Two-level cache directory structure

### 6. Health Checks and Failover
- Active health monitoring
- Automatic pod replacement
- Connection draining during updates
- Retry logic for failed requests

### 7. Best Practices

1. **Performance Optimization**
   - Enable cross-zone load balancing
   - Configure appropriate timeouts
   - Implement proper caching strategies

2. **High Availability**
   - Deploy across multiple availability zones
   - Use pod anti-affinity rules
   - Configure proper health checks

3. **Monitoring**
   - Monitor load balancer metrics
   - Track cache hit/miss ratios
   - Watch for connection errors

4. **Security**
   - Configure appropriate SSL/TLS settings
   - Implement rate limiting
   - Use proper network policies 