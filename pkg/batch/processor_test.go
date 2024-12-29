package batch

import (
	"errors"
	"testing"
	"time"
)

func TestBatchProcessor(t *testing.T) {
	config := Config{
		BatchSize:     3,
		FlushTimeout:  100 * time.Millisecond,
		MaxConcurrent: 2,
		MaxRetries:    2,
		RetryDelay:    10 * time.Millisecond,
	}

	t.Run("batch processing", func(t *testing.T) {
		processed := make([]BatchItem, 0)
		processor := func(items []BatchItem) []BatchItem {
			processed = append(processed, items...)
			for i := range items {
				items[i].Success = true
			}
			return items
		}

		bp := NewBatchProcessor(config, processor)
		bp.Start()
		defer bp.Stop()

		// Add items
		for i := 0; i < 5; i++ {
			bp.Add(BatchItem{
				ID:   string(rune(i + 65)), // A, B, C, D, E
				Data: i,
			})
		}

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		if len(processed) != 5 {
			t.Errorf("expected 5 processed items, got %d", len(processed))
		}
	})

	t.Run("batch size trigger", func(t *testing.T) {
		batchCount := 0
		processor := func(items []BatchItem) []BatchItem {
			batchCount++
			for i := range items {
				items[i].Success = true
			}
			return items
		}

		bp := NewBatchProcessor(config, processor)
		bp.Start()
		defer bp.Stop()

		// Add exactly one batch worth of items
		for i := 0; i < config.BatchSize; i++ {
			bp.Add(BatchItem{
				ID:   string(rune(i + 65)),
				Data: i,
			})
		}

		time.Sleep(50 * time.Millisecond)

		if batchCount != 1 {
			t.Errorf("expected 1 batch, got %d", batchCount)
		}
	})

	t.Run("timeout trigger", func(t *testing.T) {
		batchCount := 0
		processor := func(items []BatchItem) []BatchItem {
			batchCount++
			for i := range items {
				items[i].Success = true
			}
			return items
		}

		bp := NewBatchProcessor(config, processor)
		bp.Start()
		defer bp.Stop()

		// Add less than batch size
		bp.Add(BatchItem{
			ID:   "A",
			Data: 1,
		})

		time.Sleep(config.FlushTimeout + 50*time.Millisecond)

		if batchCount != 1 {
			t.Errorf("expected 1 batch due to timeout, got %d", batchCount)
		}
	})

	t.Run("concurrent processing limit", func(t *testing.T) {
		processing := 0
		maxConcurrent := 0
		processor := func(items []BatchItem) []BatchItem {
			processing++
			if processing > maxConcurrent {
				maxConcurrent = processing
			}
			time.Sleep(50 * time.Millisecond)
			processing--

			for i := range items {
				items[i].Success = true
			}
			return items
		}

		bp := NewBatchProcessor(config, processor)
		bp.Start()
		defer bp.Stop()

		// Add many items to trigger concurrent processing
		for i := 0; i < 10; i++ {
			bp.Add(BatchItem{
				ID:   string(rune(i + 65)),
				Data: i,
			})
		}

		time.Sleep(200 * time.Millisecond)

		if maxConcurrent > config.MaxConcurrent {
			t.Errorf("max concurrent processing exceeded limit: got %d, want <= %d",
				maxConcurrent, config.MaxConcurrent)
		}
	})

	t.Run("retry behavior", func(t *testing.T) {
		attempts := make(map[string]int)
		processor := func(items []BatchItem) []BatchItem {
			for i := range items {
				id := items[i].ID
				attempts[id]++
				if attempts[id] <= 1 {
					items[i].Success = false
					items[i].Error = errors.New("temporary error")
				} else {
					items[i].Success = true
				}
			}
			return items
		}

		bp := NewBatchProcessor(config, processor)
		bp.Start()
		defer bp.Stop()

		bp.Add(BatchItem{
			ID:   "retry-test",
			Data: 1,
		})

		time.Sleep(200 * time.Millisecond)

		if attempts["retry-test"] != 2 {
			t.Errorf("expected 2 attempts for retry, got %d", attempts["retry-test"])
		}
	})
}
