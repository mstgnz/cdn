package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/rs/zerolog"
)

// Job represents a task to be processed
type Job struct {
	ID       string
	Task     func() error
	Response chan error
}

// Pool represents a worker pool
type Pool struct {
	workers    int
	jobQueue   chan Job
	logger     zerolog.Logger
	wg         sync.WaitGroup
	ctx        context.Context
	cancelFunc context.CancelFunc
	maxRetries int
	retryDelay time.Duration
	stopOnce   sync.Once
}

// Config represents worker pool configuration
type Config struct {
	Workers    int
	QueueSize  int
	MaxRetries int
	RetryDelay time.Duration
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Workers:    config.GetEnvAsIntOrDefault("WORKER_POOL_SIZE", 5),
		QueueSize:  config.GetEnvAsIntOrDefault("WORKER_QUEUE_SIZE", 10),
		MaxRetries: config.GetEnvAsIntOrDefault("WORKER_MAX_RETRIES", 3),
		RetryDelay: time.Duration(config.GetEnvAsIntOrDefault("WORKER_RETRY_DELAY_MS", 1000)) * time.Millisecond,
	}
}

// NewPool creates a new worker pool
func NewPool(config Config) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		workers:    config.Workers,
		jobQueue:   make(chan Job, config.QueueSize),
		logger:     observability.Logger(),
		ctx:        ctx,
		cancelFunc: cancel,
		maxRetries: config.MaxRetries,
		retryDelay: config.RetryDelay,
	}
}

// Start initializes and starts the worker pool
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Stop gracefully shuts down the worker pool. It is safe to call multiple
// times; only the first call performs the shutdown.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		p.cancelFunc()

		// Wait for all jobs to complete with timeout
		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			p.logger.Info().Msg("Worker pool stopped gracefully")
		case <-time.After(30 * time.Second):
			p.logger.Warn().Msg("Worker pool stop timed out")
		}

		// Note: jobQueue is intentionally NOT closed. Workers exit via
		// ctx.Done(); closing the queue would let a concurrent Submit panic
		// with "send on closed channel".
	})
}

// Submit adds a new job to the pool
func (p *Pool) Submit(job Job) error {
	// Reject early if the pool is shutting down, before attempting the send.
	select {
	case <-p.ctx.Done():
		return fmt.Errorf("worker pool is shutting down")
	default:
	}

	select {
	case p.jobQueue <- job:
		return nil
	case <-p.ctx.Done():
		return fmt.Errorf("worker pool is shutting down")
	default:
		return fmt.Errorf("job queue is full")
	}
}

// worker processes jobs from the queue
func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case job, ok := <-p.jobQueue:
			if !ok {
				return
			}
			p.runJob(job, id)
		case <-p.ctx.Done():
			return
		}
	}
}

// runJob executes a single job with retries and delivers the result. The
// active-worker gauge is incremented/decremented per job (a plain defer in the
// worker loop would only decrement once the worker exits). The response send
// is guarded by ctx so an abandoned caller (or shutdown) cannot pin the worker
// forever on the channel send.
func (p *Pool) runJob(job Job, id int) {
	observability.WorkerPoolActiveWorkers.Inc()
	defer observability.WorkerPoolActiveWorkers.Dec()

	var err error
	retries := 0
	start := time.Now()

	for retries <= p.maxRetries {
		err = job.Task()
		duration := time.Since(start).Seconds()

		if err == nil {
			observability.WorkerJobProcessingDuration.WithLabelValues("success").Observe(duration)
			break
		}

		retries++
		observability.WorkerJobRetries.WithLabelValues("image_processing").Inc()

		p.logger.Error().
			Err(err).
			Str("jobID", job.ID).
			Int("workerID", id).
			Int("retry", retries).
			Msg("Job processing failed")

		if retries <= p.maxRetries {
			time.Sleep(p.retryDelay)
			continue
		}

		observability.WorkerJobProcessingDuration.WithLabelValues("failure").Observe(duration)
	}

	select {
	case job.Response <- err:
	case <-p.ctx.Done():
	}

	// Update queue size metric
	observability.WorkerPoolQueueSize.Set(float64(len(p.jobQueue)))
}
