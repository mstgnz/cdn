# Changelog

All notable changes to this project will be documented in this file.

## [1.7.0] - 2026-07-21

> Releases 1.4.0 through 1.6.x are not itemized here; see the GitHub Releases
> page for their notes.

### Changed / Hardening

- **Runtime robustness:** global panic-recover middleware (any handler panic → 500, no process crash); fixed a nil-index panic on uploads without a Content-Type part; hardened all Glacier AWS-SDK pointer derefs (`aws.ToString`/`aws.ToInt64` + nil guards) and added a nil `Body` guard + close (also fixes a reader leak); fixed a data race on async Glacier job state and bounded the in-memory job map.
- **Uploads:** a file with an image extension must now be a valid image (raster decoded via ImageMagick, SVG validated structurally); non-image files pass through unchanged. Honors `VALIDATE_FILE`.
- **Monitoring auth:** `/metrics` now requires a Bearer token; `/ws` requires a `token` query parameter (previously both were open).
- **Rate limiter:** retries Redis at boot and fails open (serves without rate limiting) instead of panicking the process on a Redis outage.
- **Config/secrets:** `.env` is injected via docker-compose `env_file` and excluded from the image (`.dockerignore`); `godotenv` load is now non-fatal (missing security envs are still caught by the TOKEN fail-fast). Removed a startup log that dumped the MinIO client (credential material). `MINIO_USE_SSL` is now honored.
- **DoS limits:** batch endpoints capped at `MAX_BATCH_FILES` (default 100); per-connection header buffer bounded via `READ_BUFFER_SIZE_KB` (default 1 MB, was 24 MB); GET/DELETE reject `..` path segments in object keys.

### Added

- Opt-in upload optimization: `optimize=true` on `/upload`, `/batch/upload` and
  `/upload-url` stores a visually-lossless, size-reduced image (re-encode +
  metadata strip + longest-side cap `OPTIMIZE_MAX_DIMENSION`, default 2560px).
  Animated GIFs and non-images pass through untouched; on any failure the
  original is stored so uploads never fail because of optimization.
- Process-wide ImageMagick resource limits (`IMAGICK_MEMORY_LIMIT_MB`,
  `IMAGICK_MAP_LIMIT_MB`, `IMAGICK_AREA_LIMIT_MP`, `IMAGICK_DISK_LIMIT_MB`,
  `IMAGICK_TIME_LIMIT_SEC`, `IMAGICK_WIDTH_LIMIT`, `IMAGICK_HEIGHT_LIMIT`) to
  bound decode cost and defend against decompression bombs.

### Security

- **Auth bypass fixed (breaking):** the server now fails fast on boot when
  `TOKEN` is empty, and token comparison is constant-time. Previously an empty
  server token let an empty client token pass authentication.
- **Path traversal fixed:** the Glacier async local-download target is now
  contained within `/tmp/glacier_downloads`; absolute/empty targets are
  rejected before any file is created.
- **SSRF fixed:** `/upload-url` now rejects non-http(s) schemes and
  private/loopback/link-local (incl. cloud-metadata) targets, validated both at
  request time and at dial time (DNS-rebinding safe). Opt out with
  `UPLOAD_URL_ALLOW_PRIVATE=true` for internal setups.
- **Resize DoS hardened:** on-the-fly resize width/height is clamped to
  `MAX_RESIZE_DIMENSION` (default 4096) and negative values are floored, closing
  a uint-wraparound giant-allocation path on the public GET route.
- **MIME hardening:** `X-Content-Type-Options: nosniff` on all responses and a
  restrictive `Content-Security-Policy` (sandbox) on served `.svg` objects to
  neutralize stored-XSS via SVG.

## [1.3.0] - 2024-01-15

### Added

- Secure file validation using magic bytes for enhanced security
- Improved image processing with optimized resize operations
- Redis cache integration for better performance
- AWS S3 Glacier storage class support
- Worker pool implementation for concurrent processing
- Health check endpoint with detailed status
- Prometheus metrics integration
- Circuit breaker pattern implementation:
  - Automatic failure detection and recovery
  - Configurable thresholds and timeouts
  - State monitoring via Prometheus metrics
  - Integration with AWS and storage services
- New batch operations endpoints:
  - `/batch/upload` for multiple file uploads
  - `/batch/delete` for multiple file deletions
- AWS operations made optional via `aws_upload` parameter
- Real-time monitoring via WebSocket
  - System metrics (CPU, memory, disk usage)
  - Active uploads tracking
  - Cache hit rate monitoring
  - Upload speed statistics
  - Error logs streaming
- REST endpoint for current system stats
- Batch operations for multiple file uploads/deletions

### Changed

- Refactored image processing service for better reliability
- Enhanced error handling with detailed messages
- Updated logging system with structured logs
- Improved request validation
- Optimized cache invalidation strategy
- AWS S3 operations now controlled by request parameters
- Standardized response format for batch operations

### Fixed

- Memory leak in image processing operations
- Concurrent upload handling issues
- Cache invalidation race conditions
- File type validation security issues
- Error handling in batch operations
- AWS upload parameter handling

## [1.2.0] - 2023-12-01

### Added

- Basic image processing capabilities
- MinIO storage integration
- Simple caching mechanism
- Basic error handling
- Initial API endpoints

### Changed

- Improved file upload process
- Enhanced storage handling
- Basic performance optimizations

## [1.1.0] - 2023-09-15

### Added

- Initial MinIO integration
- Basic file upload/download
- Simple authentication

## [1.0.0] - 2023-06-15

- Initial release
- Basic CDN functionality
