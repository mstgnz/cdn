package batch

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// The processor callback runs on the BatchProcessor's own goroutines, so
// anything it touches is shared state and every one of these subtests reads it
// from the test goroutine. Guarding it is not pedantry: the "concurrent
// processing limit" case in particular used an unsynchronised counter to measure
// concurrency, which is the one thing an unsynchronised counter cannot do.
type recorder struct {
	mu sync.Mutex

	items       []BatchItem
	batches     int
	attempts    map[string]int
	inFlight    int
	maxInFlight int
}

func newRecorder() *recorder {
	return &recorder{attempts: map[string]int{}}
}

func (r *recorder) recordBatch(items []BatchItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches++
	r.items = append(r.items, items...)
}

func (r *recorder) enter() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight++
	if r.inFlight > r.maxInFlight {
		r.maxInFlight = r.inFlight
	}
}

func (r *recorder) leave() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
}

func (r *recorder) countAttempt(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts[id]++
	return r.attempts[id]
}

func (r *recorder) read(fn func(*recorder)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r)
}

// waitFor polls until cond holds or the deadline passes. Used instead of a bare
// sleep so a slow machine makes the test slower rather than flaky.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func succeedAll(items []BatchItem) []BatchItem {
	for i := range items {
		items[i].Success = true
	}
	return items
}

func TestBatchProcessor(t *testing.T) {
	config := Config{
		BatchSize:     3,
		FlushTimeout:  100 * time.Millisecond,
		MaxConcurrent: 2,
		MaxRetries:    2,
		RetryDelay:    10 * time.Millisecond,
	}

	t.Run("every submitted item is processed", func(t *testing.T) {
		rec := newRecorder()
		bp := NewBatchProcessor(config, func(items []BatchItem) []BatchItem {
			rec.recordBatch(items)
			return succeedAll(items)
		})
		bp.Start()
		defer bp.Stop()

		for i := 0; i < 5; i++ {
			bp.Add(BatchItem{ID: string(rune(i + 65)), Data: i})
		}

		got := 0
		ok := waitFor(2*time.Second, func() bool {
			rec.read(func(r *recorder) { got = len(r.items) })
			return got == 5
		})
		if !ok {
			t.Errorf("expected 5 processed items, got %d", got)
		}
	})

	t.Run("a full batch flushes on size", func(t *testing.T) {
		rec := newRecorder()
		bp := NewBatchProcessor(config, func(items []BatchItem) []BatchItem {
			rec.recordBatch(items)
			return succeedAll(items)
		})
		bp.Start()
		defer bp.Stop()

		for i := 0; i < config.BatchSize; i++ {
			bp.Add(BatchItem{ID: string(rune(i + 65)), Data: i})
		}

		// Wait for the first batch, then confirm no second one appears: the
		// interesting failure is flushing too often, which polling alone would miss.
		waitFor(2*time.Second, func() bool {
			n := 0
			rec.read(func(r *recorder) { n = r.batches })
			return n >= 1
		})
		time.Sleep(config.FlushTimeout + 50*time.Millisecond)

		var batches int
		rec.read(func(r *recorder) { batches = r.batches })
		if batches != 1 {
			t.Errorf("expected exactly 1 batch, got %d", batches)
		}
	})

	t.Run("a partial batch flushes on timeout", func(t *testing.T) {
		rec := newRecorder()
		bp := NewBatchProcessor(config, func(items []BatchItem) []BatchItem {
			rec.recordBatch(items)
			return succeedAll(items)
		})
		bp.Start()
		defer bp.Stop()

		bp.Add(BatchItem{ID: "A", Data: 1})

		ok := waitFor(2*time.Second, func() bool {
			n := 0
			rec.read(func(r *recorder) { n = r.batches })
			return n == 1
		})
		if !ok {
			var batches int
			rec.read(func(r *recorder) { batches = r.batches })
			t.Errorf("expected 1 batch from the flush timeout, got %d", batches)
		}
	})

	// The cap is what keeps a burst of uploads from starting an unbounded number
	// of concurrent batches. Measuring it requires the counter to be synchronised,
	// otherwise the measurement races with the thing it is measuring.
	t.Run("concurrency stays within MaxConcurrent", func(t *testing.T) {
		rec := newRecorder()
		bp := NewBatchProcessor(config, func(items []BatchItem) []BatchItem {
			rec.enter()
			time.Sleep(50 * time.Millisecond)
			rec.leave()
			return succeedAll(items)
		})
		bp.Start()
		defer bp.Stop()

		for i := 0; i < 12; i++ {
			bp.Add(BatchItem{ID: string(rune(i + 65)), Data: i})
		}

		waitFor(3*time.Second, func() bool {
			n := 0
			rec.read(func(r *recorder) { n = len(r.items) })
			return n >= 12
		})

		var peak int
		rec.read(func(r *recorder) { peak = r.maxInFlight })
		if peak > config.MaxConcurrent {
			t.Errorf("ran %d batches at once, limit is %d", peak, config.MaxConcurrent)
		}
		if peak == 0 {
			t.Error("no batch was observed running; the test measured nothing")
		}
	})

	t.Run("a failed item is retried", func(t *testing.T) {
		rec := newRecorder()
		bp := NewBatchProcessor(config, func(items []BatchItem) []BatchItem {
			for i := range items {
				if rec.countAttempt(items[i].ID) <= 1 {
					items[i].Success = false
					items[i].Error = errors.New("temporary error")
					continue
				}
				items[i].Success = true
			}
			return items
		})
		bp.Start()
		defer bp.Stop()

		bp.Add(BatchItem{ID: "retry-test", Data: 1})

		ok := waitFor(2*time.Second, func() bool {
			n := 0
			rec.read(func(r *recorder) { n = r.attempts["retry-test"] })
			return n == 2
		})
		if !ok {
			var attempts int
			rec.read(func(r *recorder) { attempts = r.attempts["retry-test"] })
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})
}
