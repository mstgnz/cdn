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
     - Check `BodyLimit` setting (default: 25MB)
     - If using Nginx, verify `client_max_body_size` setting

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

- Error logs: `/var/log/cdn-service/error.log`
- Application logs: `/var/log/cdn-service/app.log`
- Metrics: Available via `/metrics` endpoint for Prometheus 