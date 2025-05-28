# AWS Glacier File Retrieval Guide

This project now supports retrieving and downloading files from AWS Glacier with both synchronous and asynchronous download options.

## How Does Glacier Work?

AWS Glacier is a storage service designed for long-term archiving. Files are stored as "archives" and retrieving them requires a 2-step process:

1. **Initiate Retrieval Job**: You start a file retrieval process
2. **Download File**: After the job completes, you can download the file

## Available Endpoints

### 1. Vault List

```http
GET /aws/vault-list
Authorization: Bearer YOUR_TOKEN
```

### 2. Initiate Archive Retrieval

```http
POST /aws/glacier/{vault}/initiate-retrieval/{archiveId}?type=Standard
Authorization: Bearer YOUR_TOKEN
```

**Retrieval Types:**

- `Expedited`: 1-5 minutes (expensive)
- `Standard`: 3-5 hours (standard cost)
- `Bulk`: 5-12 hours (cheap)

**Response:**

```json
{
  "status": true,
  "message": "retrieval job initiated",
  "data": {
    "jobId": "HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID",
    "location": "/123456789012/vaults/vault-name/jobs/HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID",
    "type": "Standard",
    "message": "Retrieval job started. Check status with job ID.",
    "estimatedTime": "3-5 hours"
  }
}
```

### 3. Check Job Status

```http
GET /aws/glacier/{vault}/jobs/{jobId}/status
Authorization: Bearer YOUR_TOKEN
```

**Response:**

```json
{
  "status": true,
  "message": "job status",
  "data": {
    "jobId": "HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID",
    "action": "ArchiveRetrieval",
    "statusCode": "Succeeded",
    "statusMessage": "Succeeded",
    "completed": true,
    "creationDate": "2024-01-15T10:30:00Z",
    "completionDate": "2024-01-15T15:45:00Z",
    "archiveSizeInBytes": 1048576
  }
}
```

### 4. Download Options

#### Option A: Immediate Stream Download (Synchronous)

```http
GET /aws/glacier/{vault}/jobs/{jobId}/download
Authorization: Bearer YOUR_TOKEN
```

**Use Case**: Small files or when you need immediate download.
**Note**: User must wait during the entire download process.

#### Option B: Async Download to MinIO (Recommended)

```http
POST /aws/glacier/{vault}/jobs/{jobId}/async-download
Authorization: Bearer YOUR_TOKEN
Content-Type: application/json

{
  "type": "minio",
  "targetBucket": "restored-files",
  "targetPath": "2024/01/restored-archive.jpg"
}
```

**Response:**

```json
{
  "status": true,
  "message": "async download job started",
  "data": {
    "downloadJobId": "dl-123e4567-e89b-12d3-a456-426614174000",
    "status": "pending",
    "message": "Download job has been queued. Use the download job ID to check status."
  }
}
```

**Use Case**: Large files, background processing, integration with existing MinIO buckets.

#### Option C: Async Download to Local Storage

```http
POST /aws/glacier/{vault}/jobs/{jobId}/async-download
Authorization: Bearer YOUR_TOKEN
Content-Type: application/json

{
  "type": "local",
  "targetPath": "archive-backup/file.jpg"
}
```

**Use Case**: When you need files stored locally on the server.

### 5. Check Async Download Status

```http
GET /aws/glacier/downloads/{downloadJobId}/status
Authorization: Bearer YOUR_TOKEN
```

**Response:**

```json
{
  "status": true,
  "message": "download job status",
  "data": {
    "id": "dl-123e4567-e89b-12d3-a456-426614174000",
    "vaultName": "my-vault",
    "jobId": "HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID",
    "targetBucket": "restored-files",
    "targetPath": "2024/01/restored-archive.jpg",
    "status": "completed",
    "startTime": "2024-01-15T16:00:00Z",
    "endTime": "2024-01-15T16:05:00Z",
    "downloadType": "minio"
  }
}
```

**Download Job Status Types:**

- `pending`: Job is queued
- `processing`: Download in progress
- `completed`: Download finished successfully
- `failed`: Download failed (check error field)

### 6. List All Jobs

```http
GET /aws/glacier/{vault}/jobs
Authorization: Bearer YOUR_TOKEN
```

### 7. Vault Inventory (List All Files)

```http
POST /aws/glacier/{vault}/inventory
Authorization: Bearer YOUR_TOKEN
```

When this job completes, you can get a list of all archives in the vault.

## Usage Scenarios

### Scenario 1: Small File - Immediate Download

For small files when you can wait:

```bash
# 1. Start retrieval job
curl -X POST "https://yourcdn.com/aws/glacier/my-vault/initiate-retrieval/archive123?type=Expedited" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 2. Check job status
curl "https://yourcdn.com/aws/glacier/my-vault/jobs/job456/status" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 3. Download immediately when ready
curl "https://yourcdn.com/aws/glacier/my-vault/jobs/job456/download" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -o "restored-file.ext"
```

### Scenario 2: Large File - Async Download (Recommended)

For large files or background processing:

```bash
# 1. Start retrieval job
curl -X POST "https://yourcdn.com/aws/glacier/my-vault/initiate-retrieval/archive123?type=Standard" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 2. Check job status until completed
curl "https://yourcdn.com/aws/glacier/my-vault/jobs/job456/status" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 3. Start async download to MinIO
curl -X POST "https://yourcdn.com/aws/glacier/my-vault/jobs/job456/async-download" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "minio",
    "targetBucket": "restored-files",
    "targetPath": "backups/2024/archive123.jpg"
  }'

# 4. Monitor download progress
curl "https://yourcdn.com/aws/glacier/downloads/dl-123e4567-e89b-12d3-a456-426614174000/status" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 5. Access file from MinIO when completed
curl "https://yourcdn.com/restored-files/backups/2024/archive123.jpg" \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Scenario 3: Bulk Download with Date Range

```bash
# 1. Get vault inventory
curl -X POST "https://yourcdn.com/aws/glacier/my-vault/inventory" \
  -H "Authorization: Bearer YOUR_TOKEN"

# 2. Wait for inventory job completion and download
curl "https://yourcdn.com/aws/glacier/my-vault/jobs/inventory-job-id/download" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -o "vault-inventory.json"

# 3. Parse JSON and filter by date (client-side)
# 4. Start multiple async downloads for filtered archives
```

## Date Range Filtering

AWS Glacier doesn't directly support date range filtering, but you can:

1. **Use Inventory**: Get vault inventory, which contains all archive dates
2. **Client-side filtering**: Parse the inventory JSON and filter by your desired date range
3. **Metadata storage**: Save archive IDs and dates in your own database during upload

Example inventory JSON:

```json
{
  "VaultARN": "arn:aws:glacier:us-west-2:123456789012:vaults/my-vault",
  "InventoryDate": "2024-01-15T00:00:00.000Z",
  "ArchiveList": [
    {
      "ArchiveId": "archive123",
      "ArchiveDescription": "photo.jpg",
      "CreationDate": "2024-01-10T10:30:00.000Z",
      "Size": 1048576,
      "SHA256TreeHash": "abc123..."
    }
  ]
}
```

## Performance & Best Practices

### Synchronous vs Asynchronous Downloads

**Use Synchronous Download When:**

- File size < 100MB
- Immediate access required
- Simple one-off downloads

**Use Asynchronous Download When:**

- File size > 100MB
- Multiple files to download
- Integration with existing workflows
- Server resources are limited

### Download Types Comparison

| Type       | Pros                                       | Cons                                 | Best For                     |
| ---------- | ------------------------------------------ | ------------------------------------ | ---------------------------- |
| **Stream** | Immediate download                         | User waits, server resources tied up | Small files, urgent access   |
| **MinIO**  | Background processing, integrates with CDN | Requires MinIO setup                 | Large files, production use  |
| **Local**  | Direct file system access                  | Limited by server storage            | Development, backup purposes |

### Cost Optimization

- Use **Bulk** retrieval for non-urgent large files (cheapest)
- Use **Standard** retrieval for regular use cases
- Use **Expedited** only for urgent small files (most expensive)
- Batch multiple retrievals together when possible

## Error Handling

- **202 Accepted**: Job queued successfully (async download)
- **400 Bad Request**: Missing parameters or Glacier job not completed
- **404 Not Found**: Download job ID not found
- **503 Service Unavailable**: Download queue is full
- **500 Internal Error**: Download failed (check error field in status)

## Monitoring & Troubleshooting

### Check System Resources

```http
GET /metrics
```

Monitor worker pool metrics:

- `worker_pool_queue_size`
- `worker_pool_active_workers`
- `worker_job_processing_duration_seconds`

### View Download Job History

Download job information is kept in memory. For production use, consider:

1. Persisting job status to database
2. Setting up log aggregation
3. Adding notification webhooks

With this enhanced guide, you can efficiently retrieve and download your AWS Glacier files using the most appropriate method for your use case.
