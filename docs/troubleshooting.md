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

## Logging and Monitoring

- Logs go to stdout (structured JSON via zerolog); there are no on-disk log
  files. In Docker, read them with `docker compose logs -f api`.
- Metrics: available via the `/metrics` endpoint for Prometheus. As of v1.7.0
  this endpoint requires a Bearer token (`Authorization: Bearer <TOKEN>`).
