package handler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"

	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/validator"
	"github.com/mstgnz/cdn/service"
)

// ArchiveRequest asks for specific objects to be moved to cold storage.
//
// This exists because age is not a usable signal on every deployment. Objects
// migrated into a CDN from the applications that used to hold them carry the
// migration date, not the date the content was created, so a scheduled sweep
// over "older than N days" would either delete everything or nothing. The
// application that owns the content is the only party that knows when it stops
// needing to be served from local disk, so it is the one that decides.
type ArchiveRequest struct {
	Bucket string `json:"bucket"`

	// Files accepts either an object key ("2026/photo.jpg") or a full CDN URL
	// ("https://cdn.example.com/bucket/2026/photo.jpg"). Callers usually store
	// the URL this service handed them at upload time, so requiring them to take
	// it apart again would be busywork with a chance of getting it wrong.
	Files []string `json:"files" validate:"required,min=1"`

	// Evict controls whether the local copy is removed once the archive is
	// verified to hold the object. A pointer so that omitting the field means
	// true: the point of the endpoint is to free disk, and a caller that wants
	// only to take a copy has to say so.
	Evict *bool `json:"evict"`
}

// ArchiveHandler serves the on-demand tiering endpoint.
type ArchiveHandler interface {
	ArchiveObjects(c *fiber.Ctx) error
}

type archiveHandler struct {
	tiering *service.Tiering
}

func NewArchiveHandler(tiering *service.Tiering) ArchiveHandler {
	return &archiveHandler{tiering: tiering}
}

// ArchiveObjects archives a batch of objects and, by default, frees the local
// copies. The URL each object is served from does not change: a read that misses
// locally falls through to the archive.
//
// Nothing here decides on its own that an object is safe to delete. Every file
// goes through the same verification the scheduled sweep uses, so an object the
// archive cannot confirm keeps its local copy and says why.
func (h *archiveHandler) ArchiveObjects(c *fiber.Ctx) error {
	var req ArchiveRequest
	if err := c.BodyParser(&req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, "Invalid request body", nil)
	}

	if err := validator.ValidateStruct(req); err != nil {
		return service.Response(c, fiber.StatusBadRequest, false, err.Error(), nil)
	}

	// Reconcile the body with the token before anything acts on the bucket: a
	// scoped token archives inside its own bucket and nowhere else.
	bucket, err := resolveBucket(c, req.Bucket)
	if err != nil {
		return bucketForbidden(c)
	}
	if bucket == "" {
		return service.Response(c, fiber.StatusBadRequest, false, "Bucket is required", nil)
	}

	maxBatch := config.GetEnvAsIntOrDefault("MAX_BATCH_FILES", 100)
	if maxBatch > 0 && len(req.Files) > maxBatch {
		return service.Response(c, fiber.StatusBadRequest, false,
			fmt.Sprintf("Too many files in one batch (max %d)", maxBatch), nil)
	}

	if !h.tiering.Enabled() {
		// A deployment with no archive cannot honour this request, and pretending
		// otherwise would tell the caller its files are safely stored elsewhere.
		return service.Response(c, fiber.StatusServiceUnavailable, false,
			"Archive is not configured on this deployment", nil)
	}

	// Answered once for the whole request rather than per file: a bucket left out
	// of ARCHIVE_ONLY_BUCKETS has nowhere for any of its objects to go, so a
	// per-file rejection would just repeat one configuration fact N times.
	if !h.tiering.InScope(bucket) {
		return service.Response(c, fiber.StatusBadRequest, false,
			fmt.Sprintf("Bucket %q is not in the archive scope on this deployment", bucket), nil)
	}

	evict := true
	if req.Evict != nil {
		evict = *req.Evict
	}

	results := h.archiveAll(bucket, req.Files, evict)

	summary := map[string]any{
		"archived":         0,
		"archived_kept":    0,
		"already_archived": 0,
		"not_found":        0,
		"failed":           0,
		"bytes_freed":      int64(0),
	}
	for _, r := range results {
		key := r["status"].(string)
		if n, ok := summary[key].(int); ok {
			summary[key] = n + 1
		}
		if size, ok := r["size"].(int64); ok && r["status"] == string(service.TierArchived) {
			summary["bytes_freed"] = summary["bytes_freed"].(int64) + size
		}
	}

	return service.Response(c, fiber.StatusOK, true, "success", map[string]any{
		"results": results,
		"summary": summary,
	})
}

// archiveAll runs the batch with bounded concurrency, matching the other batch
// endpoints. Each object is an independent S3 round trip, so an unbounded fan-out
// over a full batch would be a burst of connections for no gain.
func (h *archiveHandler) archiveAll(bucket string, files []string, evict bool) []map[string]any {
	ctx := context.Background()

	results := make([]map[string]any, len(files))
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for idx, file := range files {
		wg.Add(1)
		go func(idx int, file string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entry := map[string]any{"file": file}

			key, err := normalizeObjectKey(file, bucket)
			if err != nil {
				entry["status"] = string(service.TierFailed)
				entry["error"] = err.Error()
				results[idx] = entry
				return
			}

			res := h.tiering.ArchiveObject(ctx, bucket, key, evict)
			entry["object"] = key
			entry["status"] = string(res.Outcome)
			if res.Size > 0 {
				entry["size"] = res.Size
			}
			if res.Err != nil {
				entry["error"] = res.Err.Error()
			}
			results[idx] = entry
		}(idx, file)
	}

	wg.Wait()
	return results
}

// normalizeObjectKey turns whatever the caller stored into an object key.
//
// A bare key passes through. A full URL is accepted only when it points at this
// service and at the bucket the request is already authorised for: anything else
// is rejected rather than guessed at, so a link belonging to another host or
// another tenant cannot be smuggled in through a field that looks like free text.
func normalizeObjectKey(raw, bucket string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("empty file reference")
	}

	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		var err error
		value, err = keyFromURL(value, bucket)
		if err != nil {
			return "", err
		}
	}

	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return "", fmt.Errorf("no object key in %q", raw)
	}

	// Same guard the read and delete paths apply; a traversal-shaped key must not
	// reach storage whichever field it arrived in.
	if service.HasUnsafeObjectKey(value) {
		return "", fmt.Errorf("invalid object key")
	}

	return value, nil
}

// keyFromURL extracts the object key from a CDN link.
func keyFromURL(raw, bucket string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("malformed URL")
	}

	// Compare hosts, not whole origins: links are stored over years and a
	// deployment that moved from http to https should not invalidate all of them.
	// The host still has to match, so this is not an open door.
	appURL, err := url.Parse(config.GetEnvOrDefault("APP_URL", "http://localhost:9090"))
	if err == nil && appURL.Host != "" && !strings.EqualFold(parsed.Host, appURL.Host) {
		return "", fmt.Errorf("URL host %q does not belong to this CDN", parsed.Host)
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	prefix := bucket + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("URL does not point at bucket %q", bucket)
	}

	return strings.TrimPrefix(path, prefix), nil
}
