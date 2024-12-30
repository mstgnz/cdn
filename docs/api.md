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

## Rate Limits
- Global: 100 requests per minute per IP
- Upload endpoints: 10 requests per minute per IP
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
            "minio": "connected",
            "aws": "connected",
            "cache": "connected"
        },
        "timestamp": "2024-01-15T10:30:00Z"
    }
}
```

#### Metrics
```http
GET /metrics
```
Returns Prometheus format metrics.

#### WebSocket Connection
```http
GET /ws
```
Establishes WebSocket connection for real-time monitoring.

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
        "errors": [
            "Failed to process image: invalid format"
        ]
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
- `files[]`: Multiple image files (max 10)
- `bucket`: Target bucket name
- `path`: Storage path (optional)
- `aws_upload`: Boolean flag for AWS upload (optional)
- `width`: Target width in pixels (optional)
- `height`: Target height in pixels (optional)

Response:
```json
{
    "success": true,
    "message": "Batch upload successful",
    "data": [
        {
            "filename": "image1.jpg",
            "success": true,
            "result": {...}
        },
        {
            "filename": "image2.jpg",
            "success": true,
            "result": {...}
        }
    ]
}
```

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
    "aws_upload": false
}
```

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
- `aws_delete`: Boolean query parameter for AWS deletion (optional)

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
    "files": [
        "path/to/image1.jpg",
        "path/to/image2.jpg"
    ],
    "aws_delete": false
}
```

Response:
```json
{
    "success": true,
    "message": "Batch deletion successful",
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

#### AWS Bucket Operations
```http
GET /aws/bucket-list
GET /aws/:bucket/exists
GET /aws/vault-list
```

#### Minio Bucket Operations
```http
GET /minio/bucket-list
GET /minio/:bucket/exists
GET /minio/:bucket/create
GET /minio/:bucket/delete
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