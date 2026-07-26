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

3. Apply Secrets:
```bash
# Fill in the base64 values in secrets.yaml first.
kubectl apply -f secrets.yaml
```

`app_token` is required: the service fails fast at boot without a valid general
token, so the pods will crash-loop until it is set. It must be at least 32
characters and not a placeholder. Generate one with `openssl rand -hex 32`.

4. Deploy Redis:
```bash
kubectl apply -f redis.yaml
```

5. Deploy CDN Service:
```bash
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
```

## Bucket-scoped tokens

Optional. Without them the general token is the only credential, which is the
default and needs no extra setup.

A bucket-scoped token authorizes writes to exactly one bucket and is sent as
`Authorization: Bearer <bucket>:<token>`. The entries live in the `tokens_json`
key of `cdn-secrets`, which `deployment.yaml` projects into the pod as
`/app/config/tokens.json`.

```bash
cp ../config/tokens.template.json tokens.json
openssl rand -hex 32          # one token per bucket
$EDITOR tokens.json

base64 -w0 tokens.json        # macOS: base64 -i tokens.json
# paste the result as the tokens_json value in secrets.yaml
kubectl apply -f secrets.yaml
```

The file is read **once at boot**, so updating the Secret alone changes nothing
in a running pod. Restart after every change:

```bash
kubectl rollout restart deployment/cdn-service
```

An invalid file (bad JSON, a token under 32 characters, an invalid or duplicate
bucket name) stops the pod from starting rather than silently dropping
credentials, so check the logs after a rollout.

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