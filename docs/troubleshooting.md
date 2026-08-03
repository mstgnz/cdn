# Troubleshooting Guide

## Common Issues and Solutions

1. **MinIO Connection Issues**
   - Error: "Unable to connect to MinIO server"
   - Solution:
     - Ensure MinIO service is running
     - Check MinIO connection details in `.env` file
     - Verify firewall settings

2. **File Upload Issues**
   - Error: "File size exceeds limit"
   - Solution:
     - The Fiber `BodyLimit` is 100MB (matches nginx `client_max_body_size 100M`)
     - Per-file validation cap is `MAX_FILE_SIZE` (default 100MB)
     - If using Nginx, verify `client_max_body_size` matches

3. **Image Processing Issues**
   - Error: "Image processing failed"
   - Solution:
     - Verify ImageMagick is installed
     - Check if file format is supported
     - Monitor memory limits

4. **Cache Issues**
   - Error: "Redis connection failed"
   - Solution:
     - Ensure Redis service is running
     - Verify Redis connection details
     - Monitor Redis memory usage

5. **Replicas restarting, intermittent 502s**
   - Symptom: `docker inspect` shows a high `RestartCount` while nginx, MinIO and
     Redis have long uptimes. Docker reports `OOMKilled=false`, which is
     misleading: it only flags a cgroup-limit kill, never a host-wide one.
   - Confirm with `dmesg -T | grep -i oom`; a host-wide kill logs
     `constraint=CONSTRAINT_NONE ... global_oom`.
   - Cause: ImageMagick defaults to one OpenMP thread per core and allocates
     through `malloc`, so glibc keeps a per-thread arena it never returns to the
     OS and resident memory only climbs.
   - Solution: `IMAGICK_THREAD_LIMIT=2` and `MALLOC_ARENA_MAX=2` (both shipped as
     defaults since v1.10.0), plus `mem_limit` on the api service so a runaway
     replica is contained instead of the kernel picking MinIO.

6. **Archive is not storing anything**
   - The boot log says which state it is in. `archive disabled: AWS credentials
     not configured` means one of `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
     `AWS_REGION` is missing; `archive disabled by ARCHIVE_ENABLED` means someone
     turned it off deliberately.
   - A bucket outside `ARCHIVE_ONLY_BUCKETS` is skipped silently by design.
   - Uploads report the outcome in the `awsUpload` field of the response.

7. **Retention runs but frees nothing**
   - Expected on an existing deployment: objects uploaded before the archive
     existed have no archived copy, and the job refuses to delete what it cannot
     verify. Run `backfill` first.
   - Check the pass summary: `not_archived` counts objects with no copy,
     `size_mismatch` counts copies that differ in size. Both are logged
     individually with their bucket and key.
   - Also check the window. `RETENTION_DAYS` is compared against MinIO's
     `LastModified`, which on a CDN that objects were *migrated into* is the
     migration date, not the content's age. Where that is the case, drive
     archiving from `POST /archive` instead.

8. **`backfill` skips buckets**
   - `not in the archive scope` means the bucket is not in `ARCHIVE_ONLY_BUCKETS`.
   - `is not reachable` means the destination S3 bucket does not exist. Nothing
     creates it for you; the command prints the `aws s3api` invocation to run.
     With `ARCHIVE_BUCKET` unset this is usually S3's global bucket namespace:
     ordinary names are already taken by other accounts.

9. **A file downloads instead of rendering, or renders as text**
   - Content types are sniffed from the bytes, not taken from the upload.
   - Anything sniffed as HTML or XML is deliberately served as `text/plain`: the
     service never legitimately serves HTML, and announcing it would execute an
     uploaded payload on this origin. SVG is the exception and is served as
     itself under a sandboxing CSP.

## Logging and Monitoring

- Logs go to stdout (structured JSON via zerolog); there are no on-disk log
  files. In Docker, read them with `docker compose logs -f api`.
- Metrics: available via the `/metrics` endpoint for Prometheus. As of v1.7.0
  this endpoint requires a Bearer token (`Authorization: Bearer <TOKEN>`).
