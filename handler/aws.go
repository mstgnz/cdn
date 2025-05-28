package handler

import (
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mstgnz/cdn/pkg/worker"
	"github.com/mstgnz/cdn/service"
)

type AwsHandler interface {
	GlacierVaultList(c *fiber.Ctx) error
	BucketList(c *fiber.Ctx) error
	BucketExists(c *fiber.Ctx) error
	// New glacier methods
	GlacierInitiateRetrieval(c *fiber.Ctx) error
	GlacierListJobs(c *fiber.Ctx) error
	GlacierDownloadArchive(c *fiber.Ctx) error
	GlacierJobStatus(c *fiber.Ctx) error
	GlacierInventoryRetrieval(c *fiber.Ctx) error
	// New async download methods
	GlacierInitiateAsyncDownload(c *fiber.Ctx) error
	GlacierCheckDownloadStatus(c *fiber.Ctx) error
}

type awsHandler struct {
	awsService service.AwsService
	workerPool *worker.Pool
	// Track download jobs
	downloadJobs map[string]*DownloadJob
}

// DownloadJob represents an async download job
type DownloadJob struct {
	ID           string     `json:"id"`
	VaultName    string     `json:"vaultName"`
	JobID        string     `json:"jobId"`
	TargetBucket string     `json:"targetBucket,omitempty"`
	TargetPath   string     `json:"targetPath"`
	Status       string     `json:"status"` // pending, processing, completed, failed
	Error        string     `json:"error,omitempty"`
	StartTime    time.Time  `json:"startTime"`
	EndTime      *time.Time `json:"endTime,omitempty"`
	DownloadType string     `json:"downloadType"` // minio, local, stream
}

func NewAwsHandler(awsService service.AwsService) AwsHandler {
	// Initialize worker pool for async downloads
	workerConfig := worker.DefaultConfig()
	workerConfig.Workers = 3 // Limit concurrent downloads
	wp := worker.NewPool(workerConfig)
	wp.Start()

	return &awsHandler{
		awsService:   awsService,
		workerPool:   wp,
		downloadJobs: make(map[string]*DownloadJob),
	}
}

func (a awsHandler) BucketExists(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	exists := a.awsService.BucketExists(bucketName)
	if !exists {
		return service.Response(c, fiber.StatusNotFound, false, "not found", strconv.FormatBool(exists))
	}
	return service.Response(c, fiber.StatusFound, true, "found", strconv.FormatBool(exists))
}

func (a awsHandler) BucketList(c *fiber.Ctx) error {
	buckets, err := a.awsService.ListBuckets()
	if err != nil {
		return service.Response(c, fiber.StatusOK, false, err.Error(), buckets)
	}
	return service.Response(c, fiber.StatusOK, true, "buckets", buckets)
}

func (a awsHandler) GlacierVaultList(c *fiber.Ctx) error {
	return service.Response(c, fiber.StatusOK, true, "glacier vault list", a.awsService.GlacierVaultList())
}

// GlacierInitiateRetrieval starts a retrieval job for an archive
func (a awsHandler) GlacierInitiateRetrieval(c *fiber.Ctx) error {
	vaultName := c.Params("vault")
	archiveId := c.Params("archiveId")
	retrievalType := c.Query("type", "Standard") // Standard, Bulk, or Expedited

	if vaultName == "" || archiveId == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "vault name and archive ID are required", nil)
	}

	result, err := a.awsService.GlacierInitiateRetrieval(vaultName, archiveId, retrievalType)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	return service.Response(c, fiber.StatusOK, true, "retrieval job initiated", map[string]interface{}{
		"jobId":         *result.JobId,
		"location":      *result.Location,
		"type":          retrievalType,
		"message":       "Retrieval job started. Check status with job ID.",
		"estimatedTime": getEstimatedTime(retrievalType),
	})
}

// GlacierListJobs lists all jobs for a vault
func (a awsHandler) GlacierListJobs(c *fiber.Ctx) error {
	vaultName := c.Params("vault")

	if vaultName == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "vault name is required", nil)
	}

	result, err := a.awsService.GlacierListJobs(vaultName)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	return service.Response(c, fiber.StatusOK, true, "jobs listed", result.JobList)
}

// GlacierDownloadArchive downloads completed archive retrieval (immediate stream)
func (a awsHandler) GlacierDownloadArchive(c *fiber.Ctx) error {
	vaultName := c.Params("vault")
	jobId := c.Params("jobId")

	if vaultName == "" || jobId == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "vault name and job ID are required", nil)
	}

	// First check if job is completed
	jobStatus, err := a.awsService.GlacierDescribeJob(vaultName, jobId)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	if !jobStatus.Completed {
		return service.Response(c, fiber.StatusAccepted, false, "job not completed yet", map[string]interface{}{
			"status":        jobStatus.StatusCode,
			"statusMessage": *jobStatus.StatusMessage,
			"completed":     jobStatus.Completed,
		})
	}

	// Get the archive data
	result, err := a.awsService.GlacierGetJobOutput(vaultName, jobId)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	// Stream the file to client
	c.Set("Content-Type", "application/octet-stream")
	c.Set("Content-Disposition", "attachment; filename=archive_"+jobId)

	// Copy the body stream to response
	_, err = io.Copy(c, result.Body)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, "failed to stream file", nil)
	}

	return nil
}

// GlacierJobStatus checks the status of a specific job
func (a awsHandler) GlacierJobStatus(c *fiber.Ctx) error {
	vaultName := c.Params("vault")
	jobId := c.Params("jobId")

	if vaultName == "" || jobId == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "vault name and job ID are required", nil)
	}

	result, err := a.awsService.GlacierDescribeJob(vaultName, jobId)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	status := map[string]interface{}{
		"jobId":          *result.JobId,
		"jobDescription": result.JobDescription,
		"action":         result.Action,
		"statusCode":     result.StatusCode,
		"statusMessage":  *result.StatusMessage,
		"completed":      result.Completed,
		"creationDate":   *result.CreationDate,
	}

	if result.CompletionDate != nil {
		status["completionDate"] = *result.CompletionDate
	}

	if result.ArchiveSizeInBytes != nil {
		status["archiveSizeInBytes"] = *result.ArchiveSizeInBytes
	}

	return service.Response(c, fiber.StatusOK, true, "job status", status)
}

// GlacierInventoryRetrieval initiates inventory retrieval for a vault
func (a awsHandler) GlacierInventoryRetrieval(c *fiber.Ctx) error {
	vaultName := c.Params("vault")

	if vaultName == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "vault name is required", nil)
	}

	result, err := a.awsService.GlacierInventoryRetrieval(vaultName)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	return service.Response(c, fiber.StatusOK, true, "inventory retrieval job initiated", map[string]interface{}{
		"jobId":         *result.JobId,
		"location":      *result.Location,
		"message":       "Inventory retrieval job started. This will list all archives in the vault.",
		"estimatedTime": "3-5 hours",
	})
}

// GlacierInitiateAsyncDownload starts an async download job
func (a awsHandler) GlacierInitiateAsyncDownload(c *fiber.Ctx) error {
	vaultName := c.Params("vault")
	jobId := c.Params("jobId")

	// Parse request body
	var req struct {
		TargetBucket string `json:"targetBucket,omitempty"`
		TargetPath   string `json:"targetPath"`
		Type         string `json:"type"` // minio, local
	}

	if err := c.BodyParser(&req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "invalid request body", nil)
	}

	if vaultName == "" || jobId == "" || req.TargetPath == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "vault name, job ID, and target path are required", nil)
	}

	// Validate download type
	if req.Type != "minio" && req.Type != "local" {
		return service.Response(c, fiber.StatusBadRequest, false, "type must be 'minio' or 'local'", nil)
	}

	// For MinIO downloads, bucket is required
	if req.Type == "minio" && req.TargetBucket == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "target bucket is required for MinIO downloads", nil)
	}

	// Check if Glacier job is completed first
	jobStatus, err := a.awsService.GlacierDescribeJob(vaultName, jobId)
	if err != nil {
		return service.Response(c, fiber.StatusInternalServerError, false, err.Error(), nil)
	}

	if !jobStatus.Completed {
		return service.Response(c, fiber.StatusBadRequest, false, "glacier retrieval job not completed yet", map[string]interface{}{
			"status":    jobStatus.StatusCode,
			"completed": jobStatus.Completed,
		})
	}

	// Create download job
	downloadJobID := uuid.New().String()
	downloadJob := &DownloadJob{
		ID:           downloadJobID,
		VaultName:    vaultName,
		JobID:        jobId,
		TargetBucket: req.TargetBucket,
		TargetPath:   req.TargetPath,
		Status:       "pending",
		StartTime:    time.Now(),
		DownloadType: req.Type,
	}

	// Store job
	a.downloadJobs[downloadJobID] = downloadJob

	// Create worker job
	respChan := make(chan error, 1)
	workerJob := worker.Job{
		ID: downloadJobID,
		Task: func() error {
			// Update status
			downloadJob.Status = "processing"

			var err error
			if req.Type == "minio" {
				err = a.awsService.GlacierDownloadToMinio(vaultName, jobId, req.TargetBucket, req.TargetPath)
			} else {
				// Ensure local path directory exists
				localPath := filepath.Join("/tmp/glacier_downloads", req.TargetPath)
				err = a.awsService.GlacierDownloadToLocal(vaultName, jobId, localPath)
			}

			// Update job status
			now := time.Now()
			downloadJob.EndTime = &now
			if err != nil {
				downloadJob.Status = "failed"
				downloadJob.Error = err.Error()
			} else {
				downloadJob.Status = "completed"
			}

			return err
		},
		Response: respChan,
	}

	// Submit to worker pool
	if err := a.workerPool.Submit(workerJob); err != nil {
		return service.Response(c, fiber.StatusServiceUnavailable, false, "download queue is full", nil)
	}

	return service.Response(c, fiber.StatusAccepted, true, "async download job started", map[string]interface{}{
		"downloadJobId": downloadJobID,
		"status":        "pending",
		"message":       "Download job has been queued. Use the download job ID to check status.",
	})
}

// GlacierCheckDownloadStatus checks the status of an async download job
func (a awsHandler) GlacierCheckDownloadStatus(c *fiber.Ctx) error {
	downloadJobID := c.Params("downloadJobId")

	if downloadJobID == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "download job ID is required", nil)
	}

	downloadJob, exists := a.downloadJobs[downloadJobID]
	if !exists {
		return service.Response(c, fiber.StatusNotFound, false, "download job not found", nil)
	}

	return service.Response(c, fiber.StatusOK, true, "download job status", downloadJob)
}

// Helper function to estimate completion time based on retrieval type
func getEstimatedTime(retrievalType string) string {
	switch retrievalType {
	case "Expedited":
		return "1-5 minutes"
	case "Standard":
		return "3-5 hours"
	case "Bulk":
		return "5-12 hours"
	default:
		return "3-5 hours"
	}
}
