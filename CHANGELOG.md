# Changelog

All notable changes to this project will be documented in this file.

## [1.3.0] - 2024-01-15
### Added
- Secure file validation using magic bytes for enhanced security
- Improved image processing with optimized resize operations
- Redis cache integration for better performance
- AWS S3 Glacier storage class support
- Worker pool implementation for concurrent processing
- Health check endpoint with detailed status
- Prometheus metrics integration
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