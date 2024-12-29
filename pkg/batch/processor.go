package batch

import (
	"context"
	"sync"
	"time"

	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/rs/zerolog"
)

// BatchItem represents a single item in a batch
type BatchItem struct {
	ID      string
	Data    any
	Error   error
	Success bool
}

// Config represents batch processor configuration
type Config struct {
	BatchSize     int
	FlushTimeout  time.Duration
	MaxConcurrent int
	MaxRetries    int
	RetryDelay    time.Duration
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		BatchSize:     10,
		FlushTimeout:  5 * time.Second,
		MaxConcurrent: 5,
		MaxRetries:    3,
		RetryDelay:    time.Second,
	}
}

// BatchProcessor handles batch processing operations
type BatchProcessor struct {
	config     Config
	items      chan BatchItem
	processor  func([]BatchItem) []BatchItem
	logger     zerolog.Logger
	wg         sync.WaitGroup
	ctx        context.Context
	cancelFunc context.CancelFunc
	semaphore  chan struct{}
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(config Config, processor func([]BatchItem) []BatchItem) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	return &BatchProcessor{
		config:     config,
		items:      make(chan BatchItem, config.BatchSize*2),
		processor:  processor,
		logger:     observability.Logger(),
		ctx:        ctx,
		cancelFunc: cancel,
		semaphore:  make(chan struct{}, config.MaxConcurrent),
	}
}

// Start begins processing batches
func (b *BatchProcessor) Start() {
	b.wg.Add(1)
	go b.processBatches()
}

// Stop gracefully shuts down the batch processor
func (b *BatchProcessor) Stop() {
	b.cancelFunc()
	close(b.items)
	b.wg.Wait()
}

// Add adds a new item to be processed
func (b *BatchProcessor) Add(item BatchItem) {
	select {
	case b.items <- item:
	case <-b.ctx.Done():
		b.logger.Warn().Msg("Batch processor is shutting down, item rejected")
	}
}

// processBatches handles the batch processing loop
func (b *BatchProcessor) processBatches() {
	defer b.wg.Done()

	var batch []BatchItem
	timer := time.NewTimer(b.config.FlushTimeout)
	defer timer.Stop()

	for {
		select {
		case item, ok := <-b.items:
			if !ok {
				if len(batch) > 0 {
					b.processBatchWithRetry(batch)
				}
				return
			}

			batch = append(batch, item)
			if len(batch) >= b.config.BatchSize {
				b.processBatchWithRetry(batch)
				batch = nil
				timer.Reset(b.config.FlushTimeout)
			}

		case <-timer.C:
			if len(batch) > 0 {
				b.processBatchWithRetry(batch)
				batch = nil
			}
			timer.Reset(b.config.FlushTimeout)

		case <-b.ctx.Done():
			if len(batch) > 0 {
				b.processBatchWithRetry(batch)
			}
			return
		}
	}
}

func (b *BatchProcessor) processBatchWithRetry(items []BatchItem) {
	b.semaphore <- struct{}{}        // Acquire semaphore
	defer func() { <-b.semaphore }() // Release semaphore

	var processed []BatchItem
	retries := 0
	start := time.Now()

	for retries <= b.config.MaxRetries {
		processed = b.processor(items)
		duration := time.Since(start).Seconds()

		failed := 0
		success := 0
		for _, item := range processed {
			if item.Success {
				success++
			} else {
				failed++
			}
		}

		// Update metrics
		observability.BatchProcessingDuration.WithLabelValues("success").Observe(duration)
		observability.BatchItemsProcessed.WithLabelValues("success").Add(float64(success))
		observability.BatchItemsProcessed.WithLabelValues("failed").Add(float64(failed))
		observability.BatchProcessorQueueSize.Set(float64(len(b.items)))

		if failed == 0 {
			break
		}

		retries++
		observability.BatchRetries.Inc()

		if retries <= b.config.MaxRetries {
			b.logger.Warn().
				Int("retry", retries).
				Int("failed", failed).
				Msg("Retrying failed batch items")

			// Filter failed items for retry
			var failedItems []BatchItem
			for _, item := range processed {
				if !item.Success {
					failedItems = append(failedItems, item)
				}
			}
			items = failedItems
			time.Sleep(b.config.RetryDelay)
		}
	}

	// Log final results
	success := 0
	failed := 0
	for _, item := range processed {
		if item.Success {
			success++
		} else {
			failed++
			b.logger.Error().
				Err(item.Error).
				Str("itemID", item.ID).
				Msg("Batch item processing failed")
		}
	}

	b.logger.Info().
		Int("total", len(processed)).
		Int("success", success).
		Int("failed", failed).
		Msg("Batch processing completed")
}
