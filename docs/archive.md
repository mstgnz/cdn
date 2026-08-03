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
| `ARCHIVE_ONLY_BUCKETS` | empty | Empty means every bucket is archived. Comma-separated to narrow it |
| `RETENTION_ENABLED` | `false` | The retention job is opt-in |
| `RETENTION_DRY_RUN` | `true` | Report what would be deleted without deleting it |
| `RETENTION_DAYS` | `365` | Objects older than this are candidates |
| `RETENTION_INTERVAL_HOURS` | `24` | How often the sweep runs |
| `RETENTION_BUCKETS` | empty | Empty means every bucket; comma-separated to limit the sweep |

### Choosing which buckets to archive

By default every bucket is archived. `ARCHIVE_ONLY_BUCKETS` narrows it:

```bash
ARCHIVE_ONLY_BUCKETS=photos,documents
```

The default is "all" rather than an opt-in list because a deployment that turns
the archive on usually wants its data covered, and an allowlist that has to be
extended leaves every newly created bucket silently unprotected until someone
remembers it.

**The scope gates writing, never reading.** An object already in the archive is
still served from it even after its bucket is dropped from the list, so narrowing
the scope cannot break a URL that already works. What it does mean:

- Anything not yet archived in an excluded bucket stays local indefinitely.
- The retention job skips excluded buckets entirely, rather than walking millions
  of objects to warn about each one having no archived copy.
- `POST /archive` answers `400` for an excluded bucket, once for the request
  rather than once per file.
- `backfill` prints one line per excluded bucket and moves on.

The variable is deliberately not called `ARCHIVE_BUCKETS`: that is one letter
from `ARCHIVE_BUCKET`, which sets the destination, and a typo between the two
would configure something entirely different while looking correct.

### Choosing a layout

Both layouts are supported and neither is enforced. **Setting `ARCHIVE_BUCKET` is
recommended**, for four reasons.

**S3 bucket names are globally unique; MinIO's are not.** This is the one that
turns a preference into a blocker. A MinIO bucket name only has to be unique
within your own installation, so buckets end up called `media`, `uploads`,
`assets`, `dos`. In S3 the namespace is shared by every AWS account on earth, and
those names were taken years ago. With bucket parity, `CreateBucket` fails with
`BucketAlreadyExists` for exactly the buckets whose names are most ordinary, and
it fails partway through a backfill rather than up front. A single archive bucket
solves global uniqueness once and never raises it again.

**A new bucket needs no AWS work.** Uploads create a missing MinIO bucket
automatically. Under bucket parity the matching S3 bucket is not created, so
archiving for that bucket fails from its first object until someone notices and
creates it by hand. Nothing breaks loudly: the upload still succeeds, the failure
is logged, and retention correctly refuses to delete anything it cannot verify.
The result is a bucket that quietly never gets archived. With one archive bucket
a new MinIO bucket simply works.

**One IAM statement instead of one per bucket.** The policy granting this service
write access is a single `Resource` line rather than a list that has to be
extended every time a bucket is added.

**AWS limits accounts to 100 buckets by default.** Raisable to 1000 on request,
but a per-bucket layout puts you on a path toward asking.

Bucket parity is the better choice when you have a small number of
distinctively-named buckets and you want per-bucket separation in AWS, for
instance different IAM policies, replication rules or retention locks per bucket.
It is also the right choice when you are mirroring into an S3 layout that already
exists.

Whichever you choose, **choose it before the first backfill and do not change it
afterwards.** The mapping is resolved from the current configuration on every
request, so switching layouts leaves everything already archived at keys nothing
will look for again. If MinIO still holds those objects you will not notice; if
retention has already removed them, those URLs are gone.

## Two ways to archive

**On demand, via `POST /archive`.** The application that owns the content names
the objects it no longer needs served from local disk. See
[the API reference](api.md#archive-objects).

**On a schedule, via the retention job.** Everything older than
`RETENTION_DAYS` is swept automatically.

They share the same verification code, so the rule that protects the data is
identical either way. Only the trigger differs.

Which one fits depends on whether the stored timestamps mean anything. A CDN that
objects were **migrated into** carries the migration date, not the date the
content was created: a five-year-old document and yesterday's photo look the same
age, and no window can tell them apart. On such a deployment the scheduled sweep
is useless and the endpoint is the only workable trigger, because the owning
application is the only party that knows when content stops mattering.

Where objects were uploaded as they were created, the timestamps are real and the
scheduled sweep does the job with no application changes at all.

Running both is fine. Leaving `RETENTION_ENABLED=false` and driving everything
through the endpoint is also fine, and is the right choice after a migration.

## Backfilling what is already there

Archiving applies to uploads made after it was switched on. Everything already in
MinIO has no archived copy, and until it does, both the retention job and the
`/archive` endpoint will correctly refuse to free any of it. The `backfill`
command closes that gap once.

It **copies, it never moves.** There is no delete call in the program at all.
Freeing space stays a separate, deliberate act performed afterwards.

It is also **safe to interrupt and rerun.** Each object is checked against the
archive before being uploaded, so a rerun skips what is already done at the cost
of one `HeadObject` per object. It keeps no state of its own: kill it, reboot the
host, run it again next week.

```bash
# Report what would be uploaded. Changes nothing, transfers nothing.
docker compose exec api ./backfill

# Do it.
docker compose exec api ./backfill -apply

# One bucket, more concurrency, stop after a thousand objects to see how it goes.
docker compose exec api ./backfill -apply -buckets photos -workers 16 -limit 1000
```

Progress is printed on an interval and the run ends with a summary:

```
[4m12s] scanned=182400 uploaded=181902 skipped=498 failed=0 78.3 GiB  738 obj/s
```

### What to expect at scale

The cost that surprises people is **requests, not bytes**. Every object is one
`PUT` regardless of size, so a store of many small files costs far more to
backfill than its total size suggests. At roughly $0.02 per thousand requests,
ten million objects is around $200 in `PUT` charges alone, once.

Throughput is usually bounded by request rate rather than bandwidth. At 100
objects per second, ten million objects takes about 28 hours; the transfer itself
may be a fraction of the available link. Raise `-workers` until either the local
store or the uplink becomes the limit, and keep in mind that the same MinIO is
serving live traffic.

Run it inside a `screen` or `tmux` session, or with `nohup`. A run of this length
should not be tied to an SSH connection.

## Getting back out

Adopting cold storage is reversible. The `restore` command is `backfill` in the
other direction: it walks what the archive holds and writes it back into local
storage.

```bash
# Report what would be pulled back.
docker compose exec api ./restore

# Do it.
docker compose exec api ./restore -apply

# One bucket.
docker compose exec api ./restore -apply -buckets photos
```

Like `backfill`, it **copies and never deletes**. Nothing is removed from the
archive, and emptying the S3 bucket stays a separate decision you make from the
AWS console once you have confirmed local storage holds everything. Automating
that here would mean a bug in this program could destroy the last copy.

It is also safe to interrupt and rerun: an object already local at the archived
size is skipped.

Two things to check before starting:

- **Free disk space.** This pulls everything back onto the filesystem the archive
  was introduced to relieve. A restore that fills the disk takes the live service
  down with it.
- **Retrieval cost.** Reading from Glacier Instant Retrieval carries a per-GB
  charge, so a full restore of a large archive is an invoice as well as a wait.

`restore` ignores `ARCHIVE_ONLY_BUCKETS`, since pulling back a bucket that was
dropped from the scope is one of the cases it exists for. It also creates a local
bucket that no longer exists, so an archive can be restored into an empty
deployment.

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
