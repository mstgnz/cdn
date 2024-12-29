# CDN API Documentation

## Base URL
```
http://localhost:3000
```

## Authentication
All protected endpoints require an authentication token in the header:
```
Authorization: Bearer <your-token>
```

## Rate Limits
- Global: 100 requests per minute per IP
- Upload endpoints: 10 requests per minute per IP

## Endpoints

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

Response:
```json
{
    "status": true,
    "message": "Success",
    "data": {
        "url": "https://cdn.example.com/bucket/path/image.jpg"
    }
}
```

#### Upload Image with AWS
```http
POST /upload-with-aws
```
Similar to `/upload` but stores in both Minio and AWS S3.

#### Upload Image from URL
```http
POST /upload-url
```
Body:
- `url`: Source image URL
- `bucket`: Bucket name
- `path`: Storage path (optional)

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

### Monitoring
```http
GET /metrics
```
Returns Prometheus metrics for:
- HTTP request counts
- Request durations
- Image processing durations
- Storage operation durations

## Error Responses
```json
{
    "status": false,
    "message": "Error description",
    "data": null
}
```

Common HTTP Status Codes:
- 200: Success
- 400: Bad Request
- 401: Unauthorized
- 429: Too Many Requests
- 500: Internal Server Error 