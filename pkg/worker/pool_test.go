package worker

import (
	"errors"
	"testing"
	"time"
)

func TestWorkerPool(t *testing.T) {
	config := Config{
		Workers:    2,
		QueueSize:  4,
		MaxRetries: 2,
		RetryDelay: time.Millisecond * 10,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	t.Run("successful job processing", func(t *testing.T) {
		respChan := make(chan error)
		job := Job{
			ID: "test-1",
			Task: func() error {
				return nil
			},
			Response: respChan,
		}

		if err := pool.Submit(job); err != nil {
			t.Errorf("failed to submit job: %v", err)
		}

		select {
		case err := <-respChan:
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		case <-time.After(time.Second):
			t.Error("job processing timed out")
		}
	})

	t.Run("job with retries", func(t *testing.T) {
		attempts := 0
		respChan := make(chan error)
		job := Job{
			ID: "test-2",
			Task: func() error {
				attempts++
				if attempts <= 1 {
					return errors.New("temporary error")
				}
				return nil
			},
			Response: respChan,
		}

		if err := pool.Submit(job); err != nil {
			t.Errorf("failed to submit job: %v", err)
		}

		select {
		case err := <-respChan:
			if err != nil {
				t.Errorf("expected no error after retry, got %v", err)
			}
			if attempts != 2 {
				t.Errorf("expected 2 attempts, got %d", attempts)
			}
		case <-time.After(time.Second):
			t.Error("job processing timed out")
		}
	})

	t.Run("queue full", func(t *testing.T) {
		// Occupy every worker with a job that blocks until released, so the
		// queue cannot be drained while we fill it (otherwise this is timing
		// dependent: idle workers dequeue faster than we can fill).
		release := make(chan struct{})
		busy := make(chan struct{}, config.Workers)
		var responses []chan error

		for i := 0; i < config.Workers; i++ {
			resp := make(chan error, 1)
			responses = append(responses, resp)
			if err := pool.Submit(Job{
				ID: "blocker",
				Task: func() error {
					busy <- struct{}{}
					<-release
					return nil
				},
				Response: resp,
			}); err != nil {
				t.Fatalf("failed to submit blocker: %v", err)
			}
		}
		// Wait until all workers are actually busy.
		for i := 0; i < config.Workers; i++ {
			<-busy
		}

		// Fill the queue to capacity; nothing is dequeued while workers block.
		for i := 0; i < config.QueueSize; i++ {
			resp := make(chan error, 1)
			responses = append(responses, resp)
			if err := pool.Submit(Job{
				ID:       "fill-queue",
				Task:     func() error { return nil },
				Response: resp,
			}); err != nil {
				t.Fatalf("failed to fill queue slot %d: %v", i, err)
			}
		}

		// The queue is now full; the next submit must be rejected.
		if err := pool.Submit(Job{
			ID:       "overflow",
			Task:     func() error { return nil },
			Response: make(chan error, 1),
		}); err == nil {
			t.Error("expected error when queue is full, got nil")
		}

		// Release blockers and drain every queued job so the pool is idle for
		// the next subtest (otherwise the still-full queue leaks across tests).
		close(release)
		for _, r := range responses {
			select {
			case <-r:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out draining queued jobs")
			}
		}
	})

	t.Run("shutdown behavior", func(t *testing.T) {
		// Submit a long-running job
		respChan := make(chan error)
		job := Job{
			ID: "long-running",
			Task: func() error {
				time.Sleep(time.Millisecond * 500)
				return nil
			},
			Response: respChan,
		}

		if err := pool.Submit(job); err != nil {
			t.Errorf("failed to submit job: %v", err)
		}

		// Stop the pool immediately
		pool.Stop()

		// Try to submit a new job
		err := pool.Submit(Job{
			ID: "post-shutdown",
			Task: func() error {
				return nil
			},
			Response: make(chan error),
		})

		if err == nil {
			t.Error("expected error when submitting to stopped pool, got nil")
		}
	})
}
