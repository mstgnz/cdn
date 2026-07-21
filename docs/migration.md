# Upgrade Guide

This project is deployed with Docker Compose (three `api` replicas behind an
nginx load balancer, plus MinIO and Redis). Upgrades are performed by pulling
the new code and rebuilding the images — there is no `systemd` unit.

## General upgrade steps

```bash
# 1. Pull the new version
git pull

# 2. Add any new environment variables to .env using .env.example as reference

# 3. Rebuild and restart all API replicas (a few seconds of blip)
docker compose up -d --build

# 4. Verify
docker compose ps
curl -s http://localhost:${APP_PORT:-9090}/health
```

The build compiles ImageMagick and uses cgo; this is handled inside the
Dockerfile, so no local Go toolchain is required for a container deploy.

## Upgrading to v1.7.0 (breaking changes)

v1.7.0 adds opt-in upload optimization and a round of security hardening. The
following require action **before** deploying:

1. **`TOKEN` is now mandatory.** The service refuses to boot with an empty
   `TOKEN`. Ensure `.env` contains a non-empty `TOKEN` on the server.
2. **`.env` is injected via Compose `env_file`.** The `api` service now has
   `env_file: - .env`, and `.env` is excluded from the image via
   `.dockerignore`. Keep `.env` at the repo root on the server (already the
   case). `godotenv` loading is now non-fatal — configuration also comes from
   the process environment.
3. **`/metrics` now requires a Bearer token.** Update your Prometheus scrape
   config to send `Authorization: Bearer <TOKEN>`.
4. **`/ws` now requires a `?token=<TOKEN>` query parameter.** Update any
   WebSocket monitoring client.
5. **On-the-fly resize dimensions are clamped** to `MAX_RESIZE_DIMENSION`
   (default 4096). Raise it via env if you legitimately request larger sizes.

New optional environment variables (all have safe defaults, so an unchanged
`.env` still boots): `MINIO_USE_SSL`, `MAX_BATCH_FILES`, `READ_BUFFER_SIZE_KB`,
`MAX_RESIZE_DIMENSION`, `OPTIMIZE_*`, `IMAGICK_*`, `UPLOAD_URL_ALLOW_PRIVATE`.
See [`.env.example`](../.env.example) for the full list and defaults.

Nothing in the upload/get/delete HTTP contract changed: requests without the new
`optimize` parameter behave exactly as before.

## Migrating stored objects to another MinIO server

To move a bucket/project to a new MinIO instance (for example splitting two
projects onto separate VMs), use `mc mirror` server-to-server. See
[Server-to-Server Migration](./mc.md#server-to-server-migration-minio--minio)
in the MinIO Client guide.
