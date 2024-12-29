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
	Data    interface{}
	Error   error
	Success bool
}

// BatchProcessor handles batch processing operations
type BatchProcessor struct {
	batchSize    int
	flushTimeout time.Duration
	items        chan BatchItem
	processor    func([]BatchItem) []BatchItem
	logger       zerolog.Logger
	wg           sync.WaitGroup
	ctx          context.Context
	cancelFunc   context.CancelFunc
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, flushTimeout time.Duration, processor func([]BatchItem) []BatchItem) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	return &BatchProcessor{
		batchSize:    batchSize,
		flushTimeout: flushTimeout,
		items:        make(chan BatchItem, batchSize*2),
		processor:    processor,
		logger:       observability.Logger(),
		ctx:          ctx,
		cancelFunc:   cancel,
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
	timer := time.NewTimer(b.flushTimeout)
	defer timer.Stop()

	for {
		select {
		case item, ok := <-b.items:
			if !ok {
				// Process remaining items before shutting down
				if len(batch) > 0 {
					b.processBatch(batch)
				}
				return
			}

			batch = append(batch, item)
			if len(batch) >= b.batchSize {
				b.processBatch(batch)
				batch = nil
				timer.Reset(b.flushTimeout)
			}

		case <-timer.C:
			if len(batch) > 0 {
				b.processBatch(batch)
				batch = nil
			}
			timer.Reset(b.flushTimeout)

		case <-b.ctx.Done():
			if len(batch) > 0 {
				b.processBatch(batch)
			}
			return
		}
	}
}

// processBatch processes a single batch of items
func (b *BatchProcessor) processBatch(items []BatchItem) {
	start := time.Now()
	processed := b.processor(items)
	duration := time.Since(start).Seconds()

	// Record metrics
	observability.StorageOperationDuration.WithLabelValues("batch_process", "bulk").Observe(duration)

	// Log results
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
		Float64("duration", duration).
		Msg("Batch processing completed")
}
