# AWS Glacier CLI Usage Guide

This guide explains how to retrieve and download files from AWS Glacier using AWS CLI commands, as an alternative to the REST API endpoints.

## Prerequisites

### 1. Install AWS CLI

```bash
# Install AWS CLI v2
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"
unzip awscliv2.zip
sudo ./aws/install

# Verify installation
aws --version
```

### 2. Configure AWS CLI

```bash
# Configure credentials
aws configure
# AWS Access Key ID: [Your Access Key]
# AWS Secret Access Key: [Your Secret Key]
# Default region name: us-west-2
# Default output format: json

# Or export environment variables
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_DEFAULT_REGION="us-west-2"
```

## Basic Glacier Operations

### 1. List Glacier Vaults

```bash
# List all vaults in your account
aws glacier list-vaults --account-id -

# List vaults with details
aws glacier list-vaults --account-id - --output table
```

**Example Output:**

```json
{
  "VaultList": [
    {
      "VaultARN": "arn:aws:glacier:us-west-2:123456789012:vaults/my-vault",
      "VaultName": "my-vault",
      "CreationDate": "2024-01-15T10:30:00.000Z",
      "LastInventoryDate": "2024-01-20T15:45:00.000Z",
      "NumberOfArchives": 42,
      "SizeInBytes": 10485760
    }
  ]
}
```

### 2. Get Vault Description

```bash
# Get detailed information about a specific vault
aws glacier describe-vault --account-id - --vault-name my-vault
```

## Archive Retrieval Process

### Step 1: Initiate Archive Retrieval Job

```bash
# Standard retrieval (3-5 hours, standard cost)
aws glacier initiate-job \
  --account-id - \
  --vault-name my-vault \
  --job-parameters '{
    "Type": "archive-retrieval",
    "ArchiveId": "your-archive-id-here",
    "Tier": "Standard",
    "Description": "Retrieving my important archive"
  }'

# Expedited retrieval (1-5 minutes, expensive)
aws glacier initiate-job \
  --account-id - \
  --vault-name my-vault \
  --job-parameters '{
    "Type": "archive-retrieval",
    "ArchiveId": "your-archive-id-here",
    "Tier": "Expedited",
    "Description": "Urgent archive retrieval"
  }'

# Bulk retrieval (5-12 hours, cheapest)
aws glacier initiate-job \
  --account-id - \
  --vault-name my-vault \
  --job-parameters '{
    "Type": "archive-retrieval",
    "ArchiveId": "your-archive-id-here",
    "Tier": "Bulk",
    "Description": "Bulk archive retrieval"
  }'
```

**Example Response:**

```json
{
  "location": "/123456789012/vaults/my-vault/jobs/HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID",
  "jobId": "HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID"
}
```

### Step 2: Check Job Status

```bash
# Check if the job is completed
aws glacier describe-job \
  --account-id - \
  --vault-name my-vault \
  --job-id HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID

# List all jobs for a vault
aws glacier list-jobs --account-id - --vault-name my-vault
```

**Example Job Status:**

```json
{
  "JobId": "HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID",
  "JobDescription": "Retrieving my important archive",
  "Action": "ArchiveRetrieval",
  "ArchiveId": "your-archive-id-here",
  "VaultARN": "arn:aws:glacier:us-west-2:123456789012:vaults/my-vault",
  "CreationDate": "2024-01-15T10:30:00.000Z",
  "Completed": true,
  "StatusCode": "Succeeded",
  "StatusMessage": "Succeeded",
  "ArchiveSizeInBytes": 1048576,
  "InventorySizeInBytes": null,
  "SNSTopic": null,
  "CompletionDate": "2024-01-15T15:45:00.000Z",
  "SHA256TreeHash": "abc123def456...",
  "ArchiveSHA256TreeHash": "abc123def456...",
  "RetrievalByteRange": null,
  "Tier": "Standard"
}
```

### Step 3: Download the Archive

```bash
# Download the completed job output
aws glacier get-job-output \
  --account-id - \
  --vault-name my-vault \
  --job-id HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID \
  output-file.ext

# Download with checksum verification
aws glacier get-job-output \
  --account-id - \
  --vault-name my-vault \
  --job-id HkF9p6o7yjhFx-K3CGl6fuSm6VzW9T7esGQfco8nUXVYwS0jlb5gq1JZ55yHgt5vP54ZShjoQzQVVh7vEXAMPLEjobID \
  --checksum \
  output-file.ext
```

## Vault Inventory Operations

### Get Vault Inventory (List All Archives)

```bash
# Step 1: Initiate inventory retrieval job
aws glacier initiate-job \
  --account-id - \
  --vault-name my-vault \
  --job-parameters '{
    "Type": "inventory-retrieval",
    "Description": "Getting vault inventory",
    "Format": "JSON"
  }'

# Step 2: Wait for completion (usually 3-5 hours)
aws glacier describe-job \
  --account-id - \
  --vault-name my-vault \
  --job-id INVENTORY-JOB-ID

# Step 3: Download inventory when completed
aws glacier get-job-output \
  --account-id - \
  --vault-name my-vault \
  --job-id INVENTORY-JOB-ID \
  vault-inventory.json
```

**Example Inventory Output:**

```json
{
  "VaultARN": "arn:aws:glacier:us-west-2:123456789012:vaults/my-vault",
  "InventoryDate": "2024-01-15T00:00:00.000Z",
  "ArchiveList": [
    {
      "ArchiveId": "archive-id-1",
      "ArchiveDescription": "backup-2024-01-01.tar.gz",
      "CreationDate": "2024-01-01T10:30:00.000Z",
      "Size": 10485760,
      "SHA256TreeHash": "abc123def456..."
    },
    {
      "ArchiveId": "archive-id-2",
      "ArchiveDescription": "photos-2024-01-15.zip",
      "CreationDate": "2024-01-15T14:20:00.000Z",
      "Size": 52428800,
      "SHA256TreeHash": "def456ghi789..."
    }
  ]
}
```

## Automation Scripts

### 1. Background Download Manager Script

````bash
#!/bin/bash

# background-glacier-manager.sh
VAULT_NAME="my-vault"
DOWNLOAD_DIR="$HOME/glacier-downloads"
LOG_FILE="$DOWNLOAD_DIR/download.log"
PID_FILE="$DOWNLOAD_DIR/manager.pid"

# Create directories
mkdir -p "$DOWNLOAD_DIR/completed"
mkdir -p "$DOWNLOAD_DIR/logs"

# Function to log messages
log_message() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# Function to send desktop notification (Linux/macOS)
send_notification() {
    if command -v notify-send &> /dev/null; then
        # Linux
        notify-send "Glacier Download" "$1"
    elif command -v osascript &> /dev/null; then
        # macOS
        osascript -e "display notification \"$1\" with title \"Glacier Download\""
    fi
}

# Function to initiate archive retrieval and download in background
download_archive() {
    local archive_id="$1"
    local description="$2"
    local tier="${3:-Standard}"

    log_message "Starting retrieval for archive: $archive_id (Tier: $tier)"

    # Initiate job
    local job_output=$(aws glacier initiate-job \
        --account-id - \
        --vault-name "$VAULT_NAME" \
        --job-parameters "{
            \"Type\": \"archive-retrieval\",
            \"ArchiveId\": \"$archive_id\",
            \"Tier\": \"$tier\",
            \"Description\": \"$description\"
        }" 2>&1)

    if [ $? -eq 0 ]; then
        local job_id=$(echo "$job_output" | jq -r '.jobId')
        log_message "Job initiated successfully. Job ID: $job_id"

        # Start background monitoring for this job
        monitor_and_download "$archive_id" "$job_id" "$description" &

        echo "✅ Archive $archive_id: Download job started in background"
        echo "📁 Downloads will be saved to: $DOWNLOAD_DIR/completed/"
        echo "📋 Check progress: tail -f $LOG_FILE"
    else
        log_message "ERROR: Failed to initiate job for $archive_id: $job_output"
        echo "❌ Failed to start download for archive: $archive_id"
    fi
}

# Function to monitor job and download when ready
monitor_and_download() {
    local archive_id="$1"
    local job_id="$2"
    local description="$3"
    local max_attempts=288  # 24 hours with 5-minute intervals
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        sleep 300  # Wait 5 minutes
        attempt=$((attempt + 1))

        local status=$(aws glacier describe-job \
            --account-id - \
            --vault-name "$VAULT_NAME" \
            --job-id "$job_id" \
            --query 'Completed' \
            --output text 2>/dev/null)

        if [ "$status" = "True" ]; then
            log_message "Job completed for archive: $archive_id. Starting download..."

            # Download the file
            local filename="archive-${archive_id}-$(date +%Y%m%d_%H%M%S)"
            aws glacier get-job-output \
                --account-id - \
                --vault-name "$VAULT_NAME" \
                --job-id "$job_id" \
                "$DOWNLOAD_DIR/completed/$filename" 2>&1 | tee -a "$LOG_FILE"

            if [ $? -eq 0 ]; then
                log_message "✅ Successfully downloaded: $filename"
                send_notification "Download completed: $description"

                # Create info file
                cat > "$DOWNLOAD_DIR/completed/${filename}.info" << EOF
Archive ID: $archive_id
Job ID: $job_id
Description: $description
Download Date: $(date)
File Size: $(ls -lh "$DOWNLOAD_DIR/completed/$filename" | awk '{print $5}')
EOF

            else
                log_message "❌ Failed to download archive: $archive_id"
                send_notification "Download failed: $description"
            fi
            break
        elif [ "$status" = "False" ]; then
            if [ $((attempt % 12)) -eq 0 ]; then  # Log every hour
                log_message "Still waiting for job $job_id (archive: $archive_id) - Attempt $attempt/$max_attempts"
            fi
        else
            log_message "Error checking job status for $job_id"
        fi
    done

    if [ $attempt -eq $max_attempts ]; then
        log_message "❌ Timeout: Job $job_id did not complete within 24 hours"
        send_notification "Download timeout: $description"
    fi
}

# Function to check all running downloads
check_downloads() {
    echo "📊 Active Downloads Status:"
    echo "=========================="

    if [ -f "$LOG_FILE" ]; then
        echo "Recent activity:"
        tail -10 "$LOG_FILE"
        echo ""

        # Count active jobs
        local active_jobs=$(pgrep -f "monitor_and_download" | wc -l)
        echo "Active download jobs: $active_jobs"

        # Show completed downloads
        local completed=$(find "$DOWNLOAD_DIR/completed" -name "archive-*" -not -name "*.info" | wc -l)
        echo "Completed downloads: $completed"

        if [ $completed -gt 0 ]; then
            echo ""
            echo "📁 Recent downloads:"
            ls -lth "$DOWNLOAD_DIR/completed" | head -5
        fi
    else
        echo "No download activity found."
    fi
}

# Function to start bulk download from file
bulk_download() {
    local archive_file="$1"
    local tier="${2:-Bulk}"

    if [ ! -f "$archive_file" ]; then
        echo "❌ Archive IDs file not found: $archive_file"
        echo "Create a file with one archive ID per line"
        return 1
    fi

    echo "🚀 Starting bulk download process..."
    echo "📁 Tier: $tier"
    echo "📋 Reading archive IDs from: $archive_file"

    local count=0
    while IFS= read -r line; do
        # Skip empty lines and comments
        if [[ -n "$line" && ! "$line" =~ ^[[:space:]]*# ]]; then
            # Parse line: archive_id or archive_id:description
            local archive_id=$(echo "$line" | cut -d: -f1)
            local description=$(echo "$line" | cut -d: -f2-)

            if [ "$description" = "$archive_id" ]; then
                description="Archive $archive_id"
            fi

            download_archive "$archive_id" "$description" "$tier"
            count=$((count + 1))
            sleep 2  # Small delay between requests
        fi
    done < "$archive_file"

    echo ""
    echo "✅ Started $count download jobs in background"
    echo "📋 Monitor progress: $0 status"
}

# Function to stop all downloads
stop_downloads() {
    echo "🛑 Stopping all background downloads..."
    pkill -f "monitor_and_download"

    if [ -f "$PID_FILE" ]; then
        rm "$PID_FILE"
    fi

    echo "✅ All downloads stopped"
}

# Main script logic
case "$1" in
    "download")
        if [ -z "$2" ]; then
            echo "Usage: $0 download <archive-id> [description] [tier]"
            echo "Tiers: Expedited, Standard, Bulk"
            exit 1
        fi
        download_archive "$2" "${3:-Archive $2}" "${4:-Standard}"
        ;;

    "bulk")
        if [ -z "$2" ]; then
            echo "Usage: $0 bulk <archive-ids-file> [tier]"
            echo "File format: one archive ID per line, optionally with description (archive_id:description)"
            exit 1
        fi
        bulk_download "$2" "${3:-Bulk}"
        ;;

    "status")
        check_downloads
        ;;

    "stop")
        stop_downloads
        ;;

    "logs")
        if [ -f "$LOG_FILE" ]; then
            tail -f "$LOG_FILE"
        else
            echo "No log file found: $LOG_FILE"
        fi
        ;;

    *)
        echo "🏔️  Glacier Background Download Manager"
        echo "====================================="
        echo ""
        echo "Usage: $0 <command> [options]"
        echo ""
        echo "Commands:"
        echo "  download <archive-id> [description] [tier]  - Download single archive in background"
        echo "  bulk <file> [tier]                          - Bulk download from archive IDs file"
        echo "  status                                       - Check download status"
        echo "  logs                                         - Follow download logs"
        echo "  stop                                         - Stop all downloads"
        echo ""
        echo "Examples:"
        echo "  $0 download abc123def456 'My backup' Standard"
        echo "  $0 bulk archive-list.txt Bulk"
        echo "  $0 status"
        echo ""
        echo "Download directory: $DOWNLOAD_DIR"
        ;;
esac

### 2. Simple Background Download Script

```bash
#!/bin/bash

# simple-background-download.sh
# Usage: ./simple-background-download.sh <vault-name> <archive-id> [description]

VAULT_NAME="$1"
ARCHIVE_ID="$2"
DESCRIPTION="${3:-Archive $2}"
DOWNLOAD_DIR="$HOME/glacier-downloads"
LOG_FILE="$DOWNLOAD_DIR/download-${ARCHIVE_ID}.log"

if [ -z "$VAULT_NAME" ] || [ -z "$ARCHIVE_ID" ]; then
    echo "Usage: $0 <vault-name> <archive-id> [description]"
    exit 1
fi

mkdir -p "$DOWNLOAD_DIR"

# Function to run in background
background_download() {
    echo "[$(date)] Starting retrieval for archive: $ARCHIVE_ID" >> "$LOG_FILE"

    # Initiate job
    JOB_OUTPUT=$(aws glacier initiate-job \
        --account-id - \
        --vault-name "$VAULT_NAME" \
        --job-parameters "{
            \"Type\": \"archive-retrieval\",
            \"ArchiveId\": \"$ARCHIVE_ID\",
            \"Tier\": \"Standard\",
            \"Description\": \"$DESCRIPTION\"
        }" 2>&1)

    if [ $? -ne 0 ]; then
        echo "[$(date)] ERROR: Failed to initiate job: $JOB_OUTPUT" >> "$LOG_FILE"
        exit 1
    fi

    JOB_ID=$(echo "$JOB_OUTPUT" | jq -r '.jobId')
    echo "[$(date)] Job initiated: $JOB_ID" >> "$LOG_FILE"

    # Wait for completion
    while true; do
        sleep 300  # Check every 5 minutes

        STATUS=$(aws glacier describe-job \
            --account-id - \
            --vault-name "$VAULT_NAME" \
            --job-id "$JOB_ID" \
            --query 'Completed' \
            --output text 2>/dev/null)

        if [ "$STATUS" = "True" ]; then
            echo "[$(date)] Job completed. Starting download..." >> "$LOG_FILE"

            # Download
            FILENAME="$DOWNLOAD_DIR/archive-${ARCHIVE_ID}-$(date +%Y%m%d_%H%M%S)"
            aws glacier get-job-output \
                --account-id - \
                --vault-name "$VAULT_NAME" \
                --job-id "$JOB_ID" \
                "$FILENAME" >> "$LOG_FILE" 2>&1

            if [ $? -eq 0 ]; then
                echo "[$(date)] ✅ Download completed: $FILENAME" >> "$LOG_FILE"

                # Send notification
                if command -v notify-send &> /dev/null; then
                    notify-send "Glacier Download Complete" "Archive $ARCHIVE_ID downloaded successfully"
                fi
            else
                echo "[$(date)] ❌ Download failed" >> "$LOG_FILE"
            fi
            break
        fi
    done
}

# Start background process
background_download &
BACKGROUND_PID=$!

echo "🚀 Download started in background for archive: $ARCHIVE_ID"
echo "📋 Process ID: $BACKGROUND_PID"
echo "📁 Download directory: $DOWNLOAD_DIR"
echo "📝 Log file: $LOG_FILE"
echo ""
echo "Check progress: tail -f $LOG_FILE"
echo "Stop download: kill $BACKGROUND_PID"

# Save PID for later reference
echo "$BACKGROUND_PID" > "$DOWNLOAD_DIR/download-${ARCHIVE_ID}.pid"
````

### 3. System Service Setup (Linux)

```bash
#!/bin/bash

# install-glacier-service.sh
# This script sets up Glacier downloads as a system service

# Create service user (optional)
# sudo useradd -r -s /bin/false glacier-downloader

# Create systemd service file
sudo tee /etc/systemd/system/glacier-downloader.service > /dev/null << 'EOF'
[Unit]
Description=AWS Glacier Background Downloader
After=network.target

[Service]
Type=simple
User=YOUR_USERNAME
WorkingDirectory=/home/YOUR_USERNAME
ExecStart=/home/YOUR_USERNAME/glacier-scripts/background-glacier-manager.sh daemon
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
EOF

echo "📋 Systemd service created. To use:"
echo "1. Edit /etc/systemd/system/glacier-downloader.service"
echo "2. Replace YOUR_USERNAME with your actual username"
echo "3. sudo systemctl daemon-reload"
echo "4. sudo systemctl enable glacier-downloader"
echo "5. sudo systemctl start glacier-downloader"
```

### 4. Archive IDs File Examples

```bash
# Create archive-list.txt file
cat > archive-list.txt << 'EOF'
# Glacier Archive IDs for bulk download
# Format: archive_id:description or just archive_id

abc123def456:Backup January 2024
def456ghi789:Photos 2023
ghi789jkl012:Documents Archive
jkl012mno345:Video Files Backup

# Lines starting with # are ignored
# Empty lines are also ignored

mno345pqr678
pqr678stu901:Important Data 2024
EOF

echo "📝 Sample archive list created: archive-list.txt"
echo "Edit this file with your actual archive IDs"
```

## Background Download Usage Examples

### Download Single Archive in Background

```bash
# Make script executable
chmod +x background-glacier-manager.sh

# Download single archive
./background-glacier-manager.sh download abc123def456 "My important backup" Standard

# Check status
./background-glacier-manager.sh status

# Follow logs
./background-glacier-manager.sh logs
```

### Bulk Download in Background

```bash
# Create archive list file
echo "abc123def456:Backup 2024-01" > my-archives.txt
echo "def456ghi789:Photos Archive" >> my-archives.txt
echo "ghi789jkl012:Documents" >> my-archives.txt

# Start bulk download with Bulk tier (cheapest)
./background-glacier-manager.sh bulk my-archives.txt Bulk

# Monitor progress
./background-glacier-manager.sh status
```

### Using Simple Background Script

```bash
chmod +x simple-background-download.sh

# Start download in background
./simple-background-download.sh my-vault abc123def456 "Important file"

# Check specific log
tail -f ~/glacier-downloads/download-abc123def456.log

# Stop specific download
kill $(cat ~/glacier-downloads/download-abc123def456.pid)
```

### Using tmux for Session Management

```bash
# Start tmux session for downloads
tmux new-session -d -s glacier-downloads

# Run downloads in tmux
tmux send-keys -t glacier-downloads "./background-glacier-manager.sh bulk my-archives.txt" Enter

# Detach and let it run in background
# Later, reattach to check progress:
tmux attach-session -t glacier-downloads
```

### Using nohup for Simple Background Execution

```bash
# Run with nohup to survive terminal closure
nohup ./simple-background-download.sh my-vault abc123def456 "My file" > download.out 2>&1 &

# Check process
ps aux | grep glacier

# Check output
tail -f download.out
```

## Date Range Filtering with jq

### Filter Archives by Date Range

```bash
# Download inventory first
aws glacier get-job-output \
  --account-id - \
  --vault-name my-vault \
  --job-id INVENTORY-JOB-ID \
  vault-inventory.json

# Filter archives created after 2024-01-01
jq '.ArchiveList[] | select(.CreationDate >= "2024-01-01T00:00:00.000Z")' vault-inventory.json

# Filter archives created in January 2024
jq '.ArchiveList[] | select(.CreationDate >= "2024-01-01T00:00:00.000Z" and .CreationDate < "2024-02-01T00:00:00.000Z")' vault-inventory.json

# Extract archive IDs for date range
jq -r '.ArchiveList[] | select(.CreationDate >= "2024-01-01T00:00:00.000Z" and .CreationDate < "2024-02-01T00:00:00.000Z") | .ArchiveId' vault-inventory.json > january-archives.txt

# Filter by size (archives larger than 100MB)
jq '.ArchiveList[] | select(.Size > 104857600)' vault-inventory.json

# Filter by description pattern
jq '.ArchiveList[] | select(.ArchiveDescription | test("backup.*\\.tar\\.gz"))' vault-inventory.json
```

## Cost Optimization Tips

### Retrieval Tier Comparison

```bash
# Expedited (1-5 minutes) - Most expensive
# Best for: Urgent small files < 250MB
aws glacier initiate-job --job-parameters '{"Tier": "Expedited", ...}'

# Standard (3-5 hours) - Balanced cost/time
# Best for: Regular use cases
aws glacier initiate-job --job-parameters '{"Tier": "Standard", ...}'

# Bulk (5-12 hours) - Cheapest
# Best for: Large amounts of data, non-urgent
aws glacier initiate-job --job-parameters '{"Tier": "Bulk", ...}'
```

### Pricing Estimates (as of 2024)

- **Storage**: ~$0.004 per GB/month
- **Retrieval - Expedited**: ~$0.03 per GB + $0.01 per request
- **Retrieval - Standard**: ~$0.01 per GB (first 10GB/month free)
- **Retrieval - Bulk**: ~$0.0025 per GB (first 10GB/month free)

## Monitoring and Troubleshooting

### Check Account Limits

```bash
# Get account-wide information
aws glacier list-vaults --account-id - --query 'VaultList[*].[VaultName,NumberOfArchives,SizeInBytes]' --output table
```

### Monitor Job Progress

```bash
# Monitor all jobs in a vault
watch -n 300 'aws glacier list-jobs --account-id - --vault-name my-vault --query "JobList[?Completed==\`false\`].[JobId,Action,StatusCode,CreationDate]" --output table'
```

### Error Handling

```bash
# Check for failed jobs
aws glacier list-jobs \
  --account-id - \
  --vault-name my-vault \
  --query 'JobList[?StatusCode==`Failed`]'

# Get detailed error information
aws glacier describe-job \
  --account-id - \
  --vault-name my-vault \
  --job-id FAILED-JOB-ID \
  --query 'StatusMessage'
```

## Integration with CDN Service

After downloading files with AWS CLI, you can upload them to your CDN service:

```bash
# Download from Glacier
aws glacier get-job-output \
  --account-id - \
  --vault-name my-vault \
  --job-id JOB-ID \
  restored-file.jpg

# Upload to CDN service
curl -X POST "https://your-cdn.com/upload" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@restored-file.jpg" \
  -F "bucket=restored-files" \
  -F "path=glacier-restore/2024/01/"
```

## Best Practices

1. **Use Bulk retrieval** for large amounts of data
2. **Batch your requests** to minimize API calls
3. **Keep track of Archive IDs** in your own database
4. **Use inventory jobs** to understand vault contents
5. **Monitor costs** with AWS Cost Explorer
6. **Set up SNS notifications** for job completion
7. **Verify checksums** after download
8. **Clean up completed jobs** periodically

This CLI guide provides you with comprehensive tools to manage Glacier archives efficiently alongside your CDN service REST API.
