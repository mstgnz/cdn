// Command backfill copies existing objects from local storage into the archive.
//
// It exists because archiving only applies to uploads made after it was turned
// on. Everything already sitting in MinIO has no archived copy, and until it
// does, retention and the /archive endpoint will both (correctly) refuse to free
// any of it. This is the one-time job that closes that gap.
//
// # It copies, it never moves
//
// There is no delete call anywhere in this program. Local copies are left
// exactly as they were. Freeing space is a separate, deliberate act performed
// later by the retention job or the /archive endpoint, both of which re-verify
// before removing anything.
//
// # It is safe to interrupt and rerun
//
// Each object is checked against the archive before being uploaded, so a rerun
// skips everything already done at the cost of one HeadObject per object. Kill
// it, reboot the host, run it again next week: it picks up where it stopped
// without any state of its own.
//
// # Usage
//
//	backfill                          # report what would be uploaded, change nothing
//	backfill -apply                   # do it
//	backfill -apply -buckets photos   # limit to one bucket
//	backfill -apply -workers 16       # more concurrency
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
	"github.com/minio/minio-go/v7"

	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/service"
)

type stats struct {
	scanned  atomic.Int64
	uploaded atomic.Int64
	skipped  atomic.Int64
	failed   atomic.Int64
	bytes    atomic.Int64
}

func main() {
	var (
		apply     = flag.Bool("apply", false, "actually upload; without this the run only reports what it would do")
		bucketArg = flag.String("buckets", "", "comma-separated buckets to process (default: all)")
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
		fatal("archive is not configured; set AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY and AWS_REGION (and leave ARCHIVE_ENABLED unset or true)")
	}

	minioClient := service.MinioClient()
	store := service.MinioStore{Client: minioClient}
	tiering := service.NewTiering(store, archive)

	// Ctrl-C and SIGTERM stop the walk cleanly. Nothing needs to be unwound: an
	// interrupted run leaves a partially populated archive, which is precisely
	// the state a rerun is designed to continue from.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	buckets, err := targetBuckets(ctx, store, *bucketArg)
	if err != nil {
		fatal("listing buckets: " + err.Error())
	}

	mode := "DRY RUN (nothing will be uploaded; pass -apply to run for real)"
	if *apply {
		mode = "APPLYING"
	}
	fmt.Printf("%s\nbuckets: %s\nworkers: %d\n\n", mode, strings.Join(buckets, ", "), *workers)

	// Check every destination before transferring anything. Nothing creates an
	// archive bucket for you, and without this check a missing one is discovered
	// one failed upload at a time: with tens of millions of objects that is tens
	// of millions of identical errors and a run that has to be started over.
	usable, unreachable := checkDestinations(ctx, archive, buckets)
	if len(unreachable) > 0 {
		reportUnreachable(unreachable)
	}
	if len(usable) == 0 {
		fatal("no bucket has a reachable archive destination")
	}

	var s stats
	start := time.Now()
	done := make(chan struct{})
	go progress(&s, start, *every, done)

	for _, bucket := range usable {
		if ctx.Err() != nil {
			break
		}
		run(ctx, tiering, store, bucket, &s, *apply, *workers, *limit)
	}

	close(done)
	report(&s, start, *apply, ctx.Err() != nil)
}

// checkDestinations splits the buckets into those that can be archived and those
// that cannot, so one misconfigured bucket does not stop the rest.
func checkDestinations(ctx context.Context, archive service.Archive, buckets []string) (usable []string, unreachable map[string]error) {
	unreachable = map[string]error{}
	for _, b := range buckets {
		// Out of scope is a deliberate configuration, not a problem to report
		// alongside genuinely broken destinations.
		if !archive.InScope(b) {
			fmt.Printf("skipping %s: not in the archive scope (ARCHIVE_ONLY_BUCKETS)\n", b)
			continue
		}
		if err := archive.Reachable(ctx, b); err != nil {
			unreachable[b] = err
			continue
		}
		usable = append(usable, b)
	}
	return usable, unreachable
}

// reportUnreachable explains what to do about it, in the terms of whichever
// layout is configured.
func reportUnreachable(unreachable map[string]error) {
	fmt.Fprintln(os.Stderr, "\nThese buckets have no reachable archive destination and will be skipped:")
	for b, err := range unreachable {
		fmt.Fprintf(os.Stderr, "  %-24s %v\n", b, err)
	}

	region := config.GetEnvOrDefault("AWS_REGION", "$AWS_REGION")
	if target := strings.TrimSpace(config.GetEnvOrDefault("ARCHIVE_BUCKET", "")); target != "" {
		fmt.Fprintf(os.Stderr, "\nARCHIVE_BUCKET is %q. Create it once:\n", target)
		fmt.Fprint(os.Stderr, createBucketHelp(target, region))
		return
	}

	fmt.Fprint(os.Stderr, `
ARCHIVE_BUCKET is unset, so each bucket above needs an S3 bucket of the same
name. S3 bucket names are unique across every AWS account, so ordinary names are
usually already taken by someone else, and no amount of retrying will free them.

The way out is to set ARCHIVE_BUCKET to a single name you can register, which
holds every bucket under a "<minio-bucket>/" prefix. Decide before transferring
anything: the mapping is resolved fresh on every read, so changing it later
strands whatever was already archived. See docs/archive.md.
`)
}

func createBucketHelp(bucket, region string) string {
	return fmt.Sprintf(`  aws s3api create-bucket --bucket %s --region %s \
      --create-bucket-configuration LocationConstraint=%s
  aws s3api put-public-access-block --bucket %s \
      --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true

The bucket is not created automatically: region, public access blocking,
encryption and versioning are decisions that belong to whoever owns the AWS
account, and a bucket of user uploads created with defaults is worse than a
clear error.
`, bucket, region, region, bucket)
}

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

// run walks one bucket, feeding objects to a fixed pool of workers.
//
// The listing is streamed rather than collected: a bucket here can hold tens of
// millions of objects, and materialising that as a slice before starting work
// would cost gigabytes and delay the first upload by however long the full walk
// takes.
func run(ctx context.Context, tiering *service.Tiering, store service.ObjectStore, bucket string, s *stats, apply bool, workers int, limit int64) {
	objects := store.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})

	var wg sync.WaitGroup
	queue := make(chan minio.ObjectInfo, workers*4)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for obj := range queue {
				handle(ctx, tiering, bucket, obj, s, apply)
			}
		}()
	}

	for obj := range objects {
		if ctx.Err() != nil {
			break
		}
		if obj.Err != nil {
			s.failed.Add(1)
			fmt.Fprintf(os.Stderr, "list %s: %v\n", bucket, obj.Err)
			continue
		}
		if limit > 0 && s.scanned.Load() >= limit {
			break
		}
		s.scanned.Add(1)
		queue <- obj
	}

	close(queue)
	wg.Wait()
}

func handle(ctx context.Context, tiering *service.Tiering, bucket string, obj minio.ObjectInfo, s *stats, apply bool) {
	if !apply {
		// Ask the archive whether it already holds this object, but stop there.
		// That is one HeadObject per object and no transfer, which is what makes a
		// dry run over millions of objects affordable.
		ok, _, err := tiering.VerifyArchived(ctx, bucket, obj.Key, obj.Size)
		switch {
		case err != nil:
			s.failed.Add(1)
		case ok:
			s.skipped.Add(1)
		default:
			s.uploaded.Add(1)
			s.bytes.Add(obj.Size)
		}
		return
	}

	// evict=false is the whole point: this copies, it never moves.
	res := tiering.ArchiveKnownObject(ctx, bucket, obj.Key, obj.Size, false)
	switch res.Outcome {
	case service.TierArchivedKept:
		s.uploaded.Add(1)
		s.bytes.Add(obj.Size)
	case service.TierAlreadyArchived:
		s.skipped.Add(1)
	default:
		s.failed.Add(1)
		fmt.Fprintf(os.Stderr, "%s/%s: %v\n", bucket, obj.Key, res.Err)
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
			fmt.Printf("[%s] scanned=%d uploaded=%d skipped=%d failed=%d %.1f GiB  %.0f obj/s\n",
				time.Since(start).Truncate(time.Second),
				scanned, s.uploaded.Load(), s.skipped.Load(), s.failed.Load(),
				float64(s.bytes.Load())/(1<<30),
				float64(scanned)/elapsed)
		}
	}
}

func report(s *stats, start time.Time, apply, interrupted bool) {
	verb := "would upload"
	if apply {
		verb = "uploaded"
	}

	fmt.Printf("\n%s\n", strings.Repeat("-", 60))
	if interrupted {
		fmt.Println("interrupted; rerun to continue where this stopped")
	}
	fmt.Printf("elapsed  : %s\n", time.Since(start).Truncate(time.Second))
	fmt.Printf("scanned  : %d\n", s.scanned.Load())
	fmt.Printf("%-9s: %d (%.2f GiB)\n", verb, s.uploaded.Load(), float64(s.bytes.Load())/(1<<30))
	fmt.Printf("skipped  : %d (already archived)\n", s.skipped.Load())
	fmt.Printf("failed   : %d\n", s.failed.Load())

	if !apply && s.uploaded.Load() > 0 {
		fmt.Printf("\nRerun with -apply to transfer them. Nothing is ever deleted locally;\nfreeing space is done afterwards by the retention job or POST /archive.\n")
	}
	if s.failed.Load() > 0 {
		os.Exit(1)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "backfill: "+msg)
	os.Exit(1)
}
