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

## Upgrading to v1.10.0

Two things in this release need a decision before you deploy, and one needs a
decision before you ever run the backfill.

### Required before deploying

1. **Check `MINIO_BIND` / `REDIS_BIND` did not break your access.** MinIO's API
   and console (`9000`, `9001`) and Redis (`6379`) are now published on
   `127.0.0.1` instead of every interface. If anything outside the host reached
   them directly, it will stop working, and that is the point: a published Docker
   port bypasses `ufw`, so these were previously on the public internet of any
   host with a public IP. Reach them through a proxy or an SSH tunnel, or set
   `MINIO_BIND=0.0.0.0` if you genuinely need the old behaviour.

   While you are here: if those ports *were* reachable from outside, treat
   `MINIO_ROOT_PASSWORD` as exposed and rotate it.

2. **Remove `Cache-Control` from `proxy_ignore_headers`** in any reverse proxy in
   front of this service. When every decode slot is busy the service now answers
   a resize request with the original image and marks it `no-store`; a proxy that
   ignores that will cache a full-size image under a `?width=` URL for the
   lifetime of the entry.

3. **`aws_upload` stops doing anything.** Archiving is now a property of the
   deployment rather than of the request. The field is still accepted so existing
   clients do not fail validation, but it is ignored. If you relied on it to
   choose which uploads were mirrored, see `ARCHIVE_ONLY_BUCKETS` below.

### Decide before the first backfill

4. **`ARCHIVE_BUCKET`.** Blank means one S3 bucket per MinIO bucket, sharing
   names. Setting it puts everything in one bucket under a `<minio-bucket>/`
   prefix, which is recommended: S3 bucket names are unique across every AWS
   account, so ordinary names like `media` or `uploads` cannot be recreated
   there. **The mapping is resolved fresh on every request, so changing this
   later strands everything already archived.** See [Cold Storage
   Archive](./archive.md).

### Safe to ignore unless you want them

Everything else defaults to the previous behaviour:

- The archive is off unless `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and
  `AWS_REGION` are all set. A MinIO-only deployment is unchanged and says so once
  at boot.
- Retention is off (`RETENTION_ENABLED=false`) and reports without deleting the
  first time it is switched on (`RETENTION_DRY_RUN=true`).
- `IMAGICK_THREAD_LIMIT` (default 2) and `MALLOC_ARENA_MAX` in the image are what
  fixed replicas growing to several GB each and being OOM-killed. Raise the
  thread limit only if you have measured that you need to.
- `RESIZE_MAX_CONCURRENT` (default 4) bounds decodes on the read path.
- `.doc`, `.docx`, `.csv` and `.zip` are now accepted uploads.

### After deploying

Nothing in the upload/get/delete contract changed, and object URLs are stable
across archiving: an object moved to cold storage is served from the same URL.

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
