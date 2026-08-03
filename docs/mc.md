# MinIO Client (mc) Installation and Usage

MinIO Client (mc) is a command-line tool for MinIO and S3-compatible storage services.

## Installation

### With this project's Compose stack (no install)

The stack ships an `mc` container behind the `tools` profile, already pointed at
the local MinIO. The MinIO **server** image does not include `mc`, and adding it
there would mean rebuilding a pinned image, so it is a separate service.

```bash
docker compose --profile tools up -d mc

docker exec cdn-mc mc ls local
docker exec cdn-mc mc du local/photos
docker exec cdn-mc mc ls --recursive --summarize local/photos
```

One command starts it; the restart policy keeps it around afterwards. The `local`
alias is configured from `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` through the
`MC_HOST_local` environment variable, so no credentials are written to a config
file inside the container. A password containing `@`, `:`, `/`, `?` or `#` has to
be percent-encoded there, since that variable is a URL.

Pin the client with `MC_VERSION` if you run an older MinIO server: a newer `mc`
is fine for `ls`, `du` and `cp`, but `mc admin` commands are sensitive to the
server version.

### macOS (Homebrew)

```bash
brew install minio/stable/mc
```

### Linux/Unix

```bash
# Using wget
wget https://dl.min.io/client/mc/release/linux-amd64/mc
chmod +x mc
sudo mv mc /usr/local/bin/

# Using curl
curl https://dl.min.io/client/mc/release/linux-amd64/mc \
  --create-dirs \
  -o $HOME/minio-binaries/mc
chmod +x $HOME/minio-binaries/mc
export PATH=$PATH:$HOME/minio-binaries/
```

### Windows

```powershell
# Using PowerShell
Invoke-WebRequest -Uri "https://dl.min.io/client/mc/release/windows-amd64/mc.exe" -OutFile "mc.exe"
```

## Configuration

### Adding MinIO Server

```bash
# Local MinIO server
mc alias set myminio http://localhost:9000 minioadmin minioadmin

# Remote MinIO server
mc alias set myremote https://minio.example.com ACCESS_KEY SECRET_KEY

# AWS S3
mc alias set s3 https://s3.amazonaws.com ACCESS_KEY SECRET_KEY
```

### List Existing Aliases

```bash
mc alias list
```

## Basic File Operations

### List Files/Folders

```bash
# List buckets
mc ls myminio

# List bucket contents
mc ls myminio/mybucket

# Recursive listing
mc ls --recursive myminio/mybucket

# Detailed listing (size, date)
mc ls --summarize myminio/mybucket
```

### Copy Files (cp)

```bash
# Upload local file to MinIO
mc cp /path/to/file.txt myminio/mybucket/

# Download from MinIO to local
mc cp myminio/mybucket/file.txt /local/path/

# Copy between buckets
mc cp myminio/bucket1/file.txt myminio/bucket2/

# Recursive copy (folder)
mc cp --recursive /local/folder/ myminio/mybucket/folder/

# Copy with progress indicator
mc cp --progress /large/file.zip myminio/mybucket/
```

### Move Files (mv)

```bash
# Move/rename file
mc mv myminio/mybucket/old-name.txt myminio/mybucket/new-name.txt

# Move to different bucket
mc mv myminio/bucket1/file.txt myminio/bucket2/

# Move local file (copy + delete)
mc mv /local/file.txt myminio/mybucket/
```

### Delete Files (rm)

```bash
# Delete single file
mc rm myminio/mybucket/file.txt

# Recursive delete (folder)
mc rm --recursive myminio/mybucket/folder/

# Force delete (no confirmation)
mc rm --force --recursive myminio/mybucket/temp/

# Delete versioned object
mc rm --version-id VERSION_ID myminio/mybucket/file.txt
```

## Bucket Operations

### Create Bucket

```bash
# Simple bucket creation
mc mb myminio/newbucket

# With specific region
mc mb myminio/newbucket --region us-west-1
```

### Delete Bucket

```bash
# Delete empty bucket
mc rb myminio/mybucket

# Force delete (with contents)
mc rb --force myminio/mybucket
```

## Synchronization

### Two-way Synchronization

```bash
# Sync local folder to MinIO
mc mirror /local/folder/ myminio/mybucket/folder/

# Sync MinIO to local
mc mirror myminio/mybucket/folder/ /local/folder/

# Sync between different MinIO servers
mc mirror myminio/bucket1/ myremote/bucket2/

# Sync only new files
mc mirror --overwrite never /local/folder/ myminio/mybucket/

# Sync with deletions
mc mirror --remove /local/folder/ myminio/mybucket/
```

## Server-to-Server Migration (MinIO → MinIO)

Use this when moving one or more buckets from an existing MinIO to a **new MinIO
on another VM** (e.g. splitting two projects that share one CDN onto separate
servers). Data streams directly **source → destination**; you do NOT download it
to local disk and re-upload. `mc mirror` is the right tool here (not `mc cp`): it
is **resumable** and **incremental**, so re-running it copies only what changed.

Run `mc` on any host that can reach **both** MinIO endpoints (the old server or
the new VM both work).

### 1. Configure both servers as aliases

```bash
mc alias set src http://OLD_MINIO_HOST:9000 ACCESS_KEY SECRET_KEY
mc alias set dst http://NEW_MINIO_HOST:9000 ACCESS_KEY SECRET_KEY
```

### 2. Identify which buckets to move

```bash
mc ls src                                        # list all buckets
mc du src/project-bucket                          # total size to migrate
mc ls --recursive --summarize src/project-bucket  # object count + size
```

### 3. Create the target bucket and match its policy

```bash
mc mb dst/project-bucket

# If the source bucket is publicly readable, replicate the policy:
mc anonymous get src/project-bucket
# e.g. mc anonymous set download dst/project-bucket
```

### 4. Dry run, then start the real sync in the background

```bash
# Preview what would be copied (makes no changes)
mc mirror --dry-run src/project-bucket dst/project-bucket | head

# Real transfer. Run inside tmux/screen (or nohup) so it survives an SSH
# disconnect — a multi-TB sync takes a long time.
nohup mc mirror --preserve src/project-bucket dst/project-bucket > mirror.log 2>&1 &
tail -f mirror.log
```

- `--preserve` keeps object metadata/attributes.
- `mc mirror` is resumable: if it is interrupted, run the same command again and
  it copies only the missing/changed objects.

### 5. Catch up deltas while the source stays live

Re-run the same mirror as many times as needed; each pass only transfers objects
added or changed since the previous pass:

```bash
mc mirror --preserve src/project-bucket dst/project-bucket
```

### 6. Verify before cutover

```bash
mc diff src/project-bucket dst/project-bucket    # empty output = fully in sync
mc du src/project-bucket
mc du dst/project-bucket                           # sizes should match
```

`mc diff` lists objects that are missing on the destination or differ in size.

### 7. Cut over

1. Point the migrated project's app/config to the new MinIO endpoint
   (`MINIO_ENDPOINT` + credentials).
2. Run one final `mc mirror` to pick up anything written during the switch. For
   a zero-gap window you can keep a continuous sync running during cutover:
   ```bash
   mc mirror --watch --preserve src/project-bucket dst/project-bucket
   ```
3. Smoke-test a few objects on the new server (GET/serve them), then send live
   traffic to it.

### 8. Decommission the old copy (only after verifying)

```bash
mc rm --recursive --force src/project-bucket      # or keep it until confident
```

### Notes

- Repeat per bucket if the project spans several buckets.
- Bandwidth-throttling flags (to avoid saturating the link) vary by `mc`
  version — check `mc mirror --help` for `--limit-upload` / `--limit-download`.
- If the source bucket has **versioning** enabled, `mc mirror` moves only the
  current object versions; migrating full version history is a separate task
  (bucket replication) — plan it separately.

## File Information and Search

### File Information (stat)

```bash
# File details
mc stat myminio/mybucket/file.txt

# JSON format
mc stat --json myminio/mybucket/file.txt
```

### File Search (find)

```bash
# Search by filename
mc find myminio/mybucket --name "*.txt"

# Search by size (larger than 10MB)
mc find myminio/mybucket --larger 10MB

# Search by date (last 7 days)
mc find myminio/mybucket --newer-than 7d

# Combined search
mc find myminio/mybucket --name "*.log" --older-than 30d
```

## Permissions and Policies

### Bucket Policy

```bash
# Set policy (public read)
mc anonymous set public myminio/mybucket

# Remove policy
mc anonymous set none myminio/mybucket

# View policy
mc anonymous get myminio/mybucket
```

### Generate Temporary URLs

```bash
# 1-hour temporary download link
mc share download myminio/mybucket/file.txt --expire 1h

# 24-hour upload link
mc share upload myminio/mybucket/ --expire 24h
```

## Monitoring and Logs

### Event Monitoring

```bash
# Watch bucket events
mc event add myminio/mybucket arn:aws:sqs:us-east-1:123456789012:myqueue --event put,delete

# List events
mc event list myminio/mybucket
```

### Server Information

```bash
# Server info
mc admin info myminio

# Disk usage
mc du myminio/mybucket

# Version info
mc version
```

## Advanced Commands

### Batch Operations

```bash
# Parallel copying (4 threads)
mc cp --recursive --parallel 4 /large/dataset/ myminio/mybucket/

# Bandwidth limitation (1MB/s)
mc cp --limit-upload 1MB /file.zip myminio/mybucket/
```

### Encryption

```bash
# Copy with server-side encryption
mc cp --encrypt "myminio/mybucket/prefix" file.txt myminio/mybucket/
```

### Metadata Operations

```bash
# Add metadata
mc cp --attr "key1=value1,key2=value2" file.txt myminio/mybucket/

# View metadata
mc stat myminio/mybucket/file.txt
```

## Useful Tips

1. **Tab Completion**: Use tab for auto-completion in shell
2. **Alias Usage**: Use short aliases instead of long URLs
3. **Progress Monitoring**: Use `--progress` for large files
4. **JSON Output**: Use `--json` parameter for automation
5. **Dry Run**: Test with `mc mirror --dry-run` first

## Example Workflow

```bash
# 1. Configure MinIO server
mc alias set backup https://backup.company.com ACCESS_KEY SECRET_KEY

# 2. Create backup bucket
mc mb backup/daily-backups

# 3. Daily backup
mc mirror --remove /important/data/ backup/daily-backups/$(date +%Y-%m-%d)/

# 4. Clean old backups (older than 30 days)
mc find backup/daily-backups --older-than 30d --exec "mc rm {}"
```

## Most Common Commands Quick Reference

| Command        | Description        | Example                                              |
| -------------- | ------------------ | ---------------------------------------------------- |
| `mc alias set` | Configure server   | `mc alias set minio http://localhost:9000 user pass` |
| `mc ls`        | List files/buckets | `mc ls minio/bucket`                                 |
| `mc cp`        | Copy files         | `mc cp file.txt minio/bucket/`                       |
| `mc mv`        | Move/rename files  | `mc mv minio/bucket/old.txt minio/bucket/new.txt`    |
| `mc rm`        | Delete files       | `mc rm minio/bucket/file.txt`                        |
| `mc mb`        | Create bucket      | `mc mb minio/newbucket`                              |
| `mc rb`        | Delete bucket      | `mc rb minio/bucket`                                 |
| `mc mirror`    | Synchronize        | `mc mirror /local/ minio/bucket/`                    |
| `mc find`      | Search files       | `mc find minio/bucket --name "*.txt"`                |
| `mc stat`      | File info          | `mc stat minio/bucket/file.txt`                      |

## Background Operations and Session Persistence

### Large File Transfers (SSH Session Independent)

For large files (like 3TB), you need transfers to continue even if SSH session disconnects:

#### Method 1: Using `nohup`

```bash
# Run mc in background with nohup
nohup mc cp --progress /large/3TB-file.zip myremote/mybucket/ > transfer.log 2>&1 &

# Check progress
tail -f transfer.log

# Check if process is running
ps aux | grep mc
```

#### Method 2: Using `screen`

```bash
# Start screen session
screen -S minio-transfer

# Run your mc command
mc cp --progress /large/3TB-file.zip myremote/mybucket/

# Detach: Ctrl+A then D
# Reattach later: screen -r minio-transfer
```

#### Method 3: Using `tmux`

```bash
# Start tmux session
tmux new-session -d -s minio-transfer

# Run command in tmux
tmux send-keys -t minio-transfer "mc cp --progress /large/3TB-file.zip myremote/mybucket/" Enter

# Attach to session
tmux attach-session -t minio-transfer

# Detach: Ctrl+B then D
```

#### Method 4: Using `systemd` (Service)

```bash
# Create service file
sudo nano /etc/systemd/system/minio-transfer.service
```

Service file content:

```ini
[Unit]
Description=MinIO Large File Transfer
After=network.target

[Service]
Type=simple
User=your-username
WorkingDirectory=/home/your-username
ExecStart=/usr/local/bin/mc cp --progress /large/3TB-file.zip myremote/mybucket/
Restart=on-failure
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
# Enable and start service
sudo systemctl daemon-reload
sudo systemctl enable minio-transfer.service
sudo systemctl start minio-transfer.service

# Check status
sudo systemctl status minio-transfer.service

# View logs
sudo journalctl -u minio-transfer.service -f
```

### Advanced Transfer Options for Large Files

> **Note:** the exact flags below vary by `mc` version — verify with
> `mc cp --help` / `mc mirror --help` before relying on them. For whole
> buckets/folders (and for anything resumable), prefer `mc mirror` over `mc cp`;
> see [Server-to-Server Migration](#server-to-server-migration-minio--minio).
> `mc mirror` is resumable and incremental by design, so it is the correct tool
> for multi-TB folder/bucket transfers.

#### Bandwidth-limited transfer

```bash
# Limit bandwidth to avoid saturating the link (flag name depends on mc version)
nohup mc mirror --limit-upload 100Mi --preserve /large/dataset/ myremote/mybucket/ > transfer.log 2>&1 &
```

#### Monitor Transfer Progress

```bash
# Real-time log monitoring
tail -f transfer.log

# Check transfer speed and ETA
watch -n 5 'tail -10 transfer.log'

# Check network usage
watch -n 2 'iftop -t -s 10'
```

#### Folder Transfers with Resume Capability

```bash
# Use mirror for large folder transfers (auto-resume)
nohup mc mirror --progress /large/dataset/ myremote/mybucket/dataset/ > mirror.log 2>&1 &

# Mirror with remove (sync deletions)
nohup mc mirror --remove --progress /large/dataset/ myremote/mybucket/dataset/ > mirror.log 2>&1 &
```

### Transfer Management Scripts

#### Auto-restart Script

```bash
#!/bin/bash
# save as: auto-transfer.sh

SOURCE="/large/3TB-file.zip"
DEST="myremote/mybucket/"
LOG="transfer.log"

while true; do
    echo "$(date): Starting transfer..." >> $LOG
    mc cp --progress "$SOURCE" "$DEST" >> $LOG 2>&1

    if [ $? -eq 0 ]; then
        echo "$(date): Transfer completed successfully!" >> $LOG
        break
    else
        echo "$(date): Transfer failed, retrying in 60 seconds..." >> $LOG
        sleep 60
    fi
done
```

```bash
# Run script in background
nohup bash auto-transfer.sh &
```

#### Progress Monitoring Script

```bash
#!/bin/bash
# save as: monitor-transfer.sh

while true; do
    echo "=== $(date) ==="
    echo "Transfer Log (last 5 lines):"
    tail -5 transfer.log
    echo ""
    echo "Process Status:"
    ps aux | grep "mc cp" | grep -v grep
    echo ""
    echo "Network Usage:"
    iftop -t -s 5 -n
    echo "=========================="
    sleep 30
done
```

### Best Practices for Large Transfers

1. **Always use background methods** for files > 1GB
2. **Monitor disk space** on both source and destination
3. **Use progress logging** to track transfer status
4. **Set bandwidth limits** if needed: `--limit-upload 50MB`
5. **Use mirror instead of cp** for folders (better resume support)
6. **Test with small files first** to verify connectivity
7. **Keep transfer logs** for troubleshooting

### Emergency Commands

```bash
# Kill all mc processes
pkill -f "mc cp"

# Find and kill specific transfer
ps aux | grep "mc cp"
kill -9 PROCESS_ID

# Check remaining disk space
df -h

# Check transfer progress (if using mirror)
mc diff /local/path/ myremote/bucket/path/
```
