package handler

import (
	"sync"
	"testing"
	"time"
)

// The semaphore is what stops a gallery page from turning twenty thumbnail
// requests into twenty simultaneous full-image decodes. Once every slot is
// taken, the next caller must be told so rather than being let through.
func TestResizeSlotRefusesOnceCapacityIsReached(t *testing.T) {
	capacity := resizeSlotCapacity()

	releases := make([]func(), 0, capacity)
	defer func() {
		for _, release := range releases {
			release()
		}
	}()

	for i := 0; i < capacity; i++ {
		release, ok := acquireResizeSlotWithin(time.Second)
		if !ok {
			t.Fatalf("slot %d of %d was refused while capacity remained", i+1, capacity)
		}
		releases = append(releases, release)
	}

	// Every slot is now held, so this one has to give up rather than pile on.
	start := time.Now()
	release, ok := acquireResizeSlotWithin(20 * time.Millisecond)
	if ok {
		release()
		t.Fatal("a slot was handed out beyond the configured capacity")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("gave up after %v, before the wait had elapsed", elapsed)
	}
}

// Releasing has to actually return the slot, otherwise the first burst of
// traffic would permanently disable resizing for the life of the process.
func TestResizeSlotIsReusableAfterRelease(t *testing.T) {
	capacity := resizeSlotCapacity()

	releases := make([]func(), 0, capacity)
	for i := 0; i < capacity; i++ {
		release, ok := acquireResizeSlotWithin(time.Second)
		if !ok {
			t.Fatalf("could not fill the semaphore at slot %d", i+1)
		}
		releases = append(releases, release)
	}

	// Hand one back; the next caller should get it.
	releases[0]()
	releases = releases[1:]
	defer func() {
		for _, release := range releases {
			release()
		}
	}()

	release, ok := acquireResizeSlotWithin(time.Second)
	if !ok {
		t.Fatal("a released slot was not handed to the next caller")
	}
	release()
}

// A caller that has to wait should still be served once a slot frees up. The
// design deliberately queues rather than shedding load: latency is a better
// outcome than a visibly wrong image.
func TestResizeSlotWaitsRatherThanFailingImmediately(t *testing.T) {
	capacity := resizeSlotCapacity()

	releases := make([]func(), 0, capacity)
	for i := 0; i < capacity; i++ {
		release, ok := acquireResizeSlotWithin(time.Second)
		if !ok {
			t.Fatalf("could not fill the semaphore at slot %d", i+1)
		}
		releases = append(releases, release)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	var acquired bool
	go func() {
		defer wg.Done()
		release, ok := acquireResizeSlotWithin(2 * time.Second)
		acquired = ok
		if ok {
			release()
		}
	}()

	// Free a slot while the goroutine above is still waiting for one.
	time.Sleep(30 * time.Millisecond)
	releases[0]()
	releases = releases[1:]

	wg.Wait()

	for _, release := range releases {
		release()
	}

	if !acquired {
		t.Fatal("a queued caller was never served after a slot was freed")
	}
}
