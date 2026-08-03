// Command restore copies objects out of the archive back into local storage.
//
// It is the way out of cold storage. A deployment that adopted the archive and
// later wants to stop using it needs to be able to bring everything home;
// without that, turning the archive on would be a one-way door, and asking an
// operator to walk through one of those is how a good idea becomes a trap.
//
// # It copies, it never deletes
//
// Nothing is removed from the archive. Emptying the S3 bucket is a separate
// decision, made from the AWS console or CLI once local storage is confirmed to
// hold everything. Automating it here would mean a bug in this program could
// destroy the last remaining copy.
//
// # It is safe to interrupt and rerun
//
// An object already present locally at the archived size is skipped, so a rerun
// costs one stat per object and resumes where the last one stopped.
//
// # Before running
//
// Check there is room. This pulls everything back onto the disk the archive was
// introduced to relieve, and a restore that fills the filesystem takes the live
// service down with it. Reading from Glacier Instant Retrieval also carries a
// per-gigabyte retrieval charge, so a full restore of a large archive is a real
// invoice, not just time.
//
// # Usage
//
//	restore                          # report what would be pulled back
//	restore -apply                   # do it
//	restore -apply -buckets photos   # one bucket
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/mstgnz/cdn/service"
)

type stats struct {
	scanned  atomic.Int64
	restored atomic.Int64
	skipped  atomic.Int64
	failed   atomic.Int64
	bytes    atomic.Int64
}

func main() {
	var (
		apply     = flag.Bool("apply", false, "actually restore; without this the run only reports what it would do")
		bucketArg = flag.String("buckets", "", "comma-separated buckets to restore (default: all local buckets)")
		workers   = flag.Int("workers", 8, "concurrent object transfers")
		limit     = flag.Int64("limit", 0, "stop after this many objects (0 = no limit), for trial runs")
		every     = flag.Duration("progress", 10*time.Second, "how often to print progress")
	)
	flag.Parse()

	if err := godotenv.Load(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "note: .env not loaded, relying on the process environment")
	}
	if *workers < 1 {
		*workers = 1
	}

	aws := service.NewAwsService()
	archive := service.NewArchive(aws)
	if !archive.Enabled() {
		fatal("archive is not configured; there is nothing to restore from")
	}

	store := service.MinioStore{Client: service.MinioClient()}
	tiering := service.NewTiering(store, archive)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	buckets, err := targetBuckets(ctx, store, *bucketArg)
	if err != nil {
		fatal("listing buckets: " + err.Error())
	}

	mode := "DRY RUN (nothing will be written; pass -apply to run for real)"
	if *apply {
		mode = "APPLYING"
	}
	fmt.Printf("%s\nbuckets: %s\nworkers: %d\n", mode, strings.Join(buckets, ", "), *workers)
	fmt.Printf("\nNote: this writes back onto local storage and reads from cold storage.\nCheck free disk space, and expect a per-GB retrieval charge from AWS.\n\n")

	var s stats
	start := time.Now()
	done := make(chan struct{})
	go progress(&s, start, *every, done)

	for _, bucket := range buckets {
		if ctx.Err() != nil {
			break
		}
		if err := run(ctx, tiering, bucket, &s, *apply, *workers, *limit); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", bucket, err)
		}
	}

	close(done)
	report(&s, start, *apply, ctx.Err() != nil)
}

// targetBuckets defaults to the local buckets. A bucket that exists only in the
// archive has to be named explicitly, because listing every prefix in the
// archive bucket to discover them would mean walking the whole archive first.
func targetBuckets(ctx context.Context, store service.ObjectStore, arg string) ([]string, error) {
	if strings.TrimSpace(arg) != "" {
		var out []string
		for _, b := range strings.Split(arg, ",") {
			if b = strings.TrimSpace(b); b != "" {
				out = append(out, b)
			}
		}
		return out, nil
	}

	infos, err := store.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(infos))
	for _, b := range infos {
		out = append(out, b.Name)
	}
	return out, nil
}

func run(ctx context.Context, tiering *service.Tiering, bucket string, s *stats, apply bool, workers int, limit int64) error {
	if apply {
		// A bucket that was removed locally still has to have somewhere to land.
		if err := tiering.EnsureBucket(ctx, bucket); err != nil {
			return fmt.Errorf("preparing local bucket: %w", err)
		}
	}

	type item struct {
		key  string
		size int64
	}

	queue := make(chan item, workers*4)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range queue {
				handle(ctx, tiering, bucket, it.key, it.size, s, apply)
			}
		}()
	}

	walkErr := tiering.Walk(ctx, bucket, func(key string, size int64) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if limit > 0 && s.scanned.Load() >= limit {
			return context.Canceled
		}
		s.scanned.Add(1)
		queue <- item{key: key, size: size}
		return nil
	})

	close(queue)
	wg.Wait()

	if walkErr != nil && walkErr != context.Canceled && ctx.Err() == nil {
		return walkErr
	}
	return nil
}

func handle(ctx context.Context, tiering *service.Tiering, bucket, key string, size int64, s *stats, apply bool) {
	if !apply {
		// Dry run only asks whether the object is already local at the right size,
		// which costs one stat and transfers nothing.
		res := tiering.RestoreObject(ctx, bucket, key, size)
		if res.Outcome == service.TierAlreadyLocal {
			s.skipped.Add(1)
			return
		}
		s.restored.Add(1)
		s.bytes.Add(size)
		return
	}

	res := tiering.RestoreObject(ctx, bucket, key, size)
	switch res.Outcome {
	case service.TierRestored:
		s.restored.Add(1)
		s.bytes.Add(res.Size)
	case service.TierAlreadyLocal:
		s.skipped.Add(1)
	default:
		s.failed.Add(1)
		fmt.Fprintf(os.Stderr, "%s/%s: %v\n", bucket, key, res.Err)
	}
}

func progress(s *stats, start time.Time, every time.Duration, done <-chan struct{}) {
	if every <= 0 {
		return
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			elapsed := time.Since(start).Seconds()
			scanned := s.scanned.Load()
			fmt.Printf("[%s] scanned=%d restored=%d skipped=%d failed=%d %.1f GiB  %.0f obj/s\n",
				time.Since(start).Truncate(time.Second),
				scanned, s.restored.Load(), s.skipped.Load(), s.failed.Load(),
				float64(s.bytes.Load())/(1<<30),
				float64(scanned)/elapsed)
		}
	}
}

func report(s *stats, start time.Time, apply, interrupted bool) {
	verb := "would restore"
	if apply {
		verb = "restored"
	}

	fmt.Printf("\n%s\n", strings.Repeat("-", 60))
	if interrupted {
		fmt.Println("interrupted; rerun to continue where this stopped")
	}
	fmt.Printf("elapsed  : %s\n", time.Since(start).Truncate(time.Second))
	fmt.Printf("scanned  : %d\n", s.scanned.Load())
	fmt.Printf("%-13s: %d (%.2f GiB)\n", verb, s.restored.Load(), float64(s.bytes.Load())/(1<<30))
	fmt.Printf("skipped  : %d (already local)\n", s.skipped.Load())
	fmt.Printf("failed   : %d\n", s.failed.Load())

	if apply && s.failed.Load() == 0 && s.restored.Load() > 0 {
		fmt.Printf("\nNothing was removed from the archive. Once you have verified local storage\nholds everything, empty or delete the S3 bucket yourself; this program will\nnot do it for you.\n")
	}
	if s.failed.Load() > 0 {
		os.Exit(1)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "restore: "+msg)
	os.Exit(1)
}
