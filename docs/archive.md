# Cold Storage Archive

MinIO holds the recent window, S3 holds everything. A URL keeps working after its
object has been removed locally, so local disk stops being the constraint on how
long files are kept.

This is optional. With no AWS credentials configured the service runs on MinIO
alone, exactly as before, and says so once at boot. Nothing warns, nothing fails.

> Not to be confused with [`glacier-usage.md`](glacier-usage.md), which documents
> the `/aws/glacier/*` endpoints. Those drive the legacy Glacier **Vault** API,
> where objects are addressed by an opaque `archiveId` and retrieval is a job that
> takes hours. The archive described here is ordinary S3 with a cold storage
> class, addressed by the same bucket and key as MinIO. They are separate
> features and do not interact.

## How it works

```
upload  ──► MinIO  ──► S3 (Glacier Instant Retrieval)
                              ▲
read    ──► MinIO ──miss──────┘   (streamed through, never copied back)

retention ──► for each object older than the window:
                archive has it, same size?  ──yes──► delete from MinIO
                                            ──no───► keep it, log why
```

**Uploads** go to MinIO first and are mirrored to the archive afterwards. A failed
mirror does not fail the upload: the object is already durable in MinIO, and
losing an upload to a transient S3 error would be the worse trade. The failure is
reported in the response and the object simply stays ineligible for retention.

**Reads** try MinIO and fall back to the archive. Objects are not copied back on
the way out. Re-warming would let a single crawl of old content refill the disk
this feature exists to keep empty, and it would mean the retention job could no
longer reason about age alone.

**Retention** deletes local copies, and only ones the archive is confirmed to
hold at a matching size. Nothing is ever deleted from the archive.

## Why Glacier Instant Retrieval

| Class | Read | Storage cost | Notes |
|---|---|---|---|
| **Glacier Instant Retrieval** | normal `GET`, milliseconds | ~$0.004/GB-mo | what this uses |
| Glacier Flexible Retrieval | `RestoreObject` + 1 min to 12 h | ~$0.0036/GB-mo | unusable behind a URL |
| Glacier Deep Archive | `RestoreObject` + 12 to 48 h | ~$0.00099/GB-mo | legal retention only |

Prices are approximate and region-dependent. The gap between Instant and
Flexible is small enough that trading it for hours of latency on a CDN read makes
no sense, which is why the read-through fallback above is possible at all.

Two AWS billing rules matter when sizing this:

- Objects **under 128 KB are billed as 128 KB**. If the average object is smaller
  than that, the effective cost per stored byte rises sharply. Measure the size
  distribution before estimating.
- Deleting **before 90 days is billed as 90 days**. Retention windows shorter
  than that pay for storage they do not use.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | empty | All three present enables the archive |
| `ARCHIVE_ENABLED` | `true` | Off switch for deployments that have AWS credentials for other reasons |
| `ARCHIVE_BUCKET` | empty | Empty means one S3 bucket per MinIO bucket, sharing names and keys. A name puts everything in that bucket under a `<minio-bucket>/` prefix |
| `RETENTION_ENABLED` | `false` | The retention job is opt-in |
| `RETENTION_DRY_RUN` | `true` | Report what would be deleted without deleting it |
| `RETENTION_DAYS` | `365` | Objects older than this are candidates |
| `RETENTION_INTERVAL_HOURS` | `24` | How often the sweep runs |
| `RETENTION_BUCKETS` | empty | Empty means every bucket; comma-separated to limit the sweep |

`ARCHIVE_BUCKET` is worth setting once there are more than a handful of buckets:
one bucket means one lifecycle rule and one IAM statement instead of one per
MinIO bucket.

## Enabling retention safely

Retention is the only irreversible thing this service does on a timer, so it
ships off and, when switched on, reports before it acts.

1. **Check that old objects are actually archived.** Anything uploaded before the
   archive existed has no copy. So does anything uploaded by a version affected by
   the zero-byte archive bug fixed in 1.10.0. The job will refuse to delete them,
   correctly, but that means retention frees nothing until they are backfilled.

2. **Run in dry-run first.** With `RETENTION_ENABLED=true` and
   `RETENTION_DRY_RUN=true`, each pass logs what it would have done:

   ```json
   {"level":"info","scanned":184203,"eligible":50122,"deleted":48901,
    "bytes_freed":41203847213,"not_archived":1221,"size_mismatch":0,
    "errors":0,"dry_run":true,"message":"retention pass complete"}
   ```

   `deleted` is what *would* be deleted. What matters before going live is that
   `not_archived` and `size_mismatch` are explained. Every one of them is also
   logged individually with its bucket and key.

3. **Turn off dry run** once those numbers are understood.

The job deliberately does not sweep immediately at boot. A restart loop would
otherwise become a delete loop, and the pass right after a deploy is the one most
likely to be running with a window someone just mistyped.

## What protects the data

The invariant the whole design rests on: **age alone never authorises a delete.**
An object is removed from MinIO only after the archive has been asked for it by
name and has answered with a matching size.

The size comparison is not redundant with an existence check. Before 1.10.0 the
archive upload was handed a reader the MinIO upload had already drained, so the
archive filled with zero-byte objects that existed by every other measure. An
existence check alone would have authorised deleting all of them. This is covered
by `TestRetentionKeepsObjectWhenArchivedSizeDiffers`.

The other cases, all tested:

- No archived copy → keep, count as `not_archived`.
- Archive unreachable → keep, count as `errors`. An outage postpones the sweep,
  it never licenses it.
- Archive off → the job refuses to run at all. Retention without an archive is
  just deletion.
- `Archive` has no delete method, so the job structurally cannot remove an
  archived copy.
