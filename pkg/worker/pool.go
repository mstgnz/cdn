package worker

import (
	"context"
	"strconv"
	"sync"
	"time"

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
}

// NewPool creates a new worker pool
func NewPool(workers int) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		workers:    workers,
		jobQueue:   make(chan Job, workers*2),
		logger:     observability.Logger(),
		ctx:        ctx,
		cancelFunc: cancel,
	}
}

// Start initializes and starts the worker pool
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Stop gracefully shuts down the worker pool
func (p *Pool) Stop() {
	p.cancelFunc()
	close(p.jobQueue)
	p.wg.Wait()
}

// Submit adds a new job to the pool
func (p *Pool) Submit(job Job) {
	select {
	case p.jobQueue <- job:
	case <-p.ctx.Done():
		p.logger.Warn().Msg("Worker pool is shutting down, job rejected")
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

			// Process the job and measure duration
			start := time.Now()
			err := job.Task()
			duration := time.Since(start).Seconds()

			// Record metrics
			observability.ImageProcessingDuration.WithLabelValues("worker_" + strconv.Itoa(id)).Observe(duration)

			if err != nil {
				p.logger.Error().
					Err(err).
					Str("jobID", job.ID).
					Int("workerID", id).
					Msg("Job processing failed")
			}

			job.Response <- err

		case <-p.ctx.Done():
			return
		}
	}
}
