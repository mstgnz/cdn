# CDN API Documentation

## Base URL

```
http://localhost:9090
```

## Authentication

All protected endpoints require an authentication token in the header:

```
Authorization: Bearer <your-token>
```

The server requires a non-empty `TOKEN` environment variable and refuses to boot without it. An empty token is never accepted (no anonymous fallback).

## Rate Limits

- Global: 100 requests per minute per IP (`RATE_LIMIT`)
- Upload endpoints: 50 requests per minute per IP (`UPLOAD_RATE_LIMIT`)
- Rate limit bypass protection enabled

### Rate Limit Headers

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1640995200
```

## Standardized Response Format

### Success Response

```json
{
    "success": true,
    "message": "success",
    "data": {
        "minioUpload": "Minio Successfully Uploaded size {size}",
        "minioResult": {...},
        "awsUpload": "S3 Successfully Uploaded",
        "awsResult": "{result}",
        "imageName": "{filename}",
        "objectName": "{path/filename}",
        "link": "{url}"
    }
}
```

### Error Response

```json
{
  "success": false,
  "message": "Error description",
  "error": {
    "code": "ERROR_CODE",
    "details": "Detailed error information"
  }
}
```

## Endpoints

### System Operations

#### Health Check

```http
GET /health
```

Returns health status of all services (MinIO, AWS, Cache).

Response:

```json
{
  "success": true,
  "message": "Health check",
  "data": {
    "status": "healthy",
    "services": {
      "minio": "healthy",
      "aws": "healthy",
      "cache": "healthy"
    },
    "timestamp": "2024-01-15T10:30:00Z"
  }
}
```

Top-level `status` is `healthy` or `degraded` (503). Each service value is
`healthy` or `unhealthy: <error>`. AWS is informational only — overall health is
based on MinIO + cache.

#### Metrics

```http
GET /metrics
```

Returns Prometheus format metrics. Requires authentication: send the token as a Bearer header (`Authorization: Bearer <token>`), e.g. Prometheus `authorization.credentials`.

#### WebSocket Connection

```http
GET /ws?token=<your-token>
```

Establishes WebSocket connection for real-time monitoring. Requires the token as a `token` query parameter (WebSocket clients cannot set an Authorization header).

#### Monitor Stats

```http
GET /monitor
```

Returns current system statistics.

Response:

```json
{
  "success": true,
  "message": "success",
  "data": {
    "timestamp": "2024-01-15T10:30:00Z",
    "active_uploads": 5,
    "upload_speed": 1048576,
    "cache_hit_rate": 85.5,
    "cpu_usage": 45.2,
    "memory_usage": 60.8,
    "disk_usage": {
      "/data": 75,
      "/uploads": 45
    },
    "errors": ["Failed to process image: invalid format"]
  }
}
```

### Image Operations

#### Get Image

```http
GET /:bucket/*
GET /:bucket/w::width/*
GET /:bucket/h::height/*
GET /:bucket/w::width/h::height/*
```

Parameters:

- `bucket`: Bucket name
- `width`: Image width (optional)
- `height`: Image height (optional)
- `*`: Image path

Response: Image file or error message

#### Upload Image

```http
POST /upload
```

Headers:

- `Content-Type: multipart/form-data`
- `Authorization: Bearer <token>`

Body:

- `file`: Image file
- `bucket`: Bucket name
- `path`: Storage path (optional)
- `aws_upload`: Boolean flag for AWS upload (optional)
- `optimize`: Boolean; when `true`, store a visually-lossless, size-reduced version (re-encode + metadata strip + longest side capped at `OPTIMIZE_MAX_DIMENSION`, default 2560px). Explicit `width`/`height` take precedence over the cap. Animated GIFs and non-images pass through untouched. Default `false` stores the original bytes unchanged. (optional)
- `width`: Target width in pixels (optional)
- `height`: Target height in pixels (optional)

Response: Standard success response

#### Batch Upload

```http
POST /batch/upload
```

Headers:

- `Content-Type: multipart/form-data`
- `Authorization: Bearer <token>`

Body:

- `files`: Multiple image files (capped at `MAX_BATCH_FILES`, default 100)
- `bucket`: Target bucket name
- `path`: Storage path (optional)
- `aws_upload`: Boolean flag for AWS upload (optional)
- `optimize`: Boolean; when `true`, each uploaded image is stored size-reduced (visually lossless). Animated GIFs and non-images pass through untouched. Default `false`. (optional)

Response:

```json
{
  "success": true,
  "message": "Batch upload completed",
  "data": [
    {
      "filename": "image1.jpg",
      "success": true,
      "object_name": "uuid_image1.jpg"
    },
    {
      "filename": "image2.jpg",
      "success": true,
      "object_name": "uuid_image2.jpg"
    }
  ]
}
```

Each item includes `filename`, `success`, and `object_name`. On failure it
carries `error` instead; `aws_error` and `size` appear when relevant.

#### Upload from URL

```http
POST /upload-url
```

Headers:

- `Content-Type: application/json`
- `Authorization: Bearer <token>`

Body:

```json
{
  "url": "https://example.com/image.jpg",
  "bucket": "my-bucket",
  "path": "optional/path",
  "aws_upload": false,
  "optimize": false
}
```

`optimize` (optional, default `false`): when `true`, the downloaded image is stored size-reduced (visually lossless). Only `http`/`https` URLs to public hosts are accepted; private, loopback and cloud-metadata addresses are rejected (SSRF guard, override with `UPLOAD_URL_ALLOW_PRIVATE=true`).

Response: Standard success response

#### Resize Image

```http
POST /resize
```

Headers:

- `Content-Type: multipart/form-data`
- `Authorization: Bearer <token>`

Body:

- `file`: Image file
- `width`: Target width in pixels (optional)
- `height`: Target height in pixels (optional)

Response: Resized image file

#### Delete Image

```http
DELETE /:bucket/*
```

Parameters:

- `bucket`: Bucket name
- `*`: Image path

> Note: this endpoint deletes from MinIO only. The `aws_delete` flag is not wired
> on this route, so AWS S3 deletion is not triggered here — use `/batch/delete`
> (which honors `aws_delete` in its JSON body) for S3 removal.

Response: Standard success response

#### Batch Delete

```http
DELETE /batch/delete
```

Headers:

- `Content-Type: application/json`
- `Authorization: Bearer <token>`

Body:

```json
{
  "bucket": "my-bucket",
  "files": ["path/to/image1.jpg", "path/to/image2.jpg"],
  "aws_delete": false
}
```

Response:

```json
{
  "success": true,
  "message": "Batch delete completed",
  "data": [
    {
      "filename": "image1.jpg",
      "success": true,
      "error": null
    },
    {
      "filename": "image2.jpg",
      "success": true,
      "error": null
    }
  ]
}
```

### Storage Operations

All `/aws/*` and `/minio/*` routes require authentication (`Authorization: Bearer <token>`).

#### AWS Bucket Operations

```http
GET /aws/bucket-list
GET /aws/:bucket/exists
GET /aws/vault-list
```

#### Minio Bucket Operations

```http
GET    /minio/bucket-list
GET    /minio/:bucket/exists
GET    /minio/:bucket/create
DELETE /minio/:bucket/delete
```

## Error Codes

- `RATE_LIMIT_EXCEEDED`: Request rate limit exceeded
- `INVALID_TOKEN`: Authentication token is invalid
- `BUCKET_NOT_FOUND`: Specified bucket does not exist
- `FILE_TOO_LARGE`: Uploaded file exceeds size limit
- `INVALID_FILE_TYPE`: Unsupported file type
- `STORAGE_ERROR`: Error during storage operation
- `AWS_UPLOAD_FAILED`: AWS S3 upload failed
- `MINIO_UPLOAD_FAILED`: MinIO upload failed
- `BATCH_SIZE_EXCEEDED`: Too many files in batch operation
- `BATCH_OPERATION_FAILED`: Batch operation partially failed
- `CIRCUIT_BREAKER_OPEN`: Service temporarily unavailable
- `TOO_MANY_REQUESTS`: Concurrent request limit exceeded
