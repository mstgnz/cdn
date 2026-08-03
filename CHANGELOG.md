# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Fixed

- **A slow Redis start left replicas permanently without a cache.** Redis replays
  its RDB before it will answer commands, so on a cold start of the whole stack
  the API pings while Redis is still loading and gets back `LOADING Redis is
  loading the dataset in memory`. `NewCacheService` returned an error for that,
  and `cmd/main.go` responded by discarding the client and setting the service to
  `nil`. Nothing ever retried, so a few seconds of startup became the state of
  the process until somebody restarted it. On the 2026-08-03 deploy two of three
  replicas came up this way.

  Discarding the client was never necessary: go-redis dials lazily per command
  and reconnects on its own, so a client whose first ping failed still works the
  moment Redis accepts connections. `NewCacheService` now pings up to three times
  with backoff and, if that still fails, returns the usable service *alongside*
  the error. A nil service is now reserved for a `REDIS_URL` that cannot be
  parsed, which is configuration that can never work.

- **`/health` returned 500 with no explanation when the cache was missing.**
  `CacheService` is an interface, so the `nil` above was not caught by any check;
  `checkCacheHealth` dereferenced it and panicked, and the recover middleware
  turned that into a bare 500. It now reports `unhealthy: cache not configured`,
  which still fails the core health check but says why.

- **`POST /resize` did the work and threw the result away.** It decoded the
  upload, resized it, checked the output was not nil, discarded it, and answered
  with a JSON `Image processed successfully` carrying no image. Both `docs/api.md`
  and the OpenAPI spec already documented the endpoint as returning the resized
  image directly, so this was the code drifting from its own contract rather than
  an undocumented design. `processImage` now stores the output on the request and
  the handler sends those bytes with a sniffed, inert `Content-Type`.

## [1.11.0] - 2026-08-04

1.10.0 tightened upload validation. This release fixes the parts of it that
rejected real files, which is the failure operators work around by turning
validation off entirely.

### Fixed

- **HEIC, MP4, MOV, 3GP and TIFF uploads were rejected under extensions the
  allowlist accepted.** These are ISO base media files (ISO/IEC 14496-12) and
  none of them has a fixed signature at offset 0. What identifies them is the
  literal `ftyp` at offset 4, preceded by that box's *length*. The magic number
  table treated the length as part of the signature, pinning `0x18` for HEIC and
  MP4 and `0x14` for 3GP, so a file passed only when its first box happened to be
  exactly 24 or 20 bytes long. ImageMagick writes `0x1C` for HEIC, ffmpeg writes
  `0x20` for MP4, QuickTime writes `0x14` with a `qt` brand, and iPhone HEIC
  varies by device. All were refused as `INVALID_FILE_CONTENT` while
  `AllowedFileFormats` said the extension was fine, which is worse than not
  supporting the format: the upload fails talking about content while the
  documentation talks about extensions. TIFF had no entry at all and could never
  pass in either byte order.

  `isISOBaseMedia` now identifies the family structurally, by `ftyp` at offset 4,
  and TIFF is recognised as `II*\0` and `MM\0*`. This is a container check, not a
  codec check, and it widens nothing: the extension allowlist is what keeps
  executable types out and it is unchanged.

- **The MIME type gate rejected valid uploads and stopped nothing.**
  `ValidateFile` checked the multipart part's `Content-Type` against
  `AllowedMimeTypes`, but that string is written by the client, so as a gate it
  never had teeth. `application/octet-stream` has to appear on any such list
  because mobile clients send it for everything, and that one entry was a
  documented bypass: a payload announced as octet-stream sailed through. What it
  did do was refuse a valid PNG whose part carried no `Content-Type` header at
  all, and a valid PDF announced as `application/x-pdf`. Callers that pass a
  browser-supplied type straight through hit this routinely, and the only
  workaround available to an operator was `VALIDATE_FILE=false`, which turns off
  the two checks that were doing the work. A gate whose failure mode is "disable
  all validation" is worse than no gate.

  The check is removed. `AllowedMimeTypes` is kept as documentation of what
  callers typically send; nothing reads it.

- **An empty `config/tokens.json` stopped the service from booting.** The line
  that matters is between "no scoped tokens" and "scoped tokens that cannot be
  read", not between "no file" and "some file". Missing, empty, `{}` and
  `{"buckets": []}` now all boot with zero scoped tokens, because each is an
  operator saying there are none yet and none of them can be hiding a credential.
  Content that cannot be parsed, or an entry that fails validation, still stops
  boot: there the operator wrote definitions and believes they are active, so
  starting with fewer credentials than configured would surface as callers
  getting "invalid token" with nothing to explain it. The general `TOKEN` is
  unaffected either way, validated separately and accepted on every route
  whatever this file contains.

- **Placeholder AWS credentials switched the archive on.** Copying
  `.env.example` left `AWS_ACCESS_KEY_ID=your-aws-key` in place, which is not
  empty, so the archive reported itself enabled and then failed against a
  destination that never existed. The shipped placeholders now count as
  unconfigured alongside the existing empty check.

### Added

- **`MINIO_VERSION` pins the MinIO server image.** Blank still means `latest`,
  which is fine for a fresh install and not fine for an existing one: MinIO
  migrates its on-disk format forward and never backward, so an accidental jump
  is not something you undo by changing the tag back.

- **The archive destination is verified at boot** when archiving is enabled.
  Credentials that look valid stay looking valid until something uses them;
  `VerifyDestination` makes one real call and logs the result. It runs off the
  startup path on purpose, so boot never waits on AWS and a transient outage
  during a restart cannot disable archiving for the life of the process. It
  reports only and changes no behaviour.

### Changed

- `.env.example` is ordered by how much attention each setting needs, from
  "nothing works until this is right" down to values that can be ignored for
  years, with the reasoning for the non-obvious ones written beside them.

- The example host nginx config (`cdn.conf`) is rewritten: TLS and redirects, the
  proxy headers the service actually reads, a `log_format` that records the
  vhost, a cache zone, and an `expires` map that respects a `no-store` from
  upstream rather than overwriting it. Caching in front of this service means a
  deleted object keeps being served until its entry expires, and the open source
  nginx build has no purge.

- `docs/api.md` describes two upload gates rather than three, and its extension
  table no longer lists `tif`, `ico` and `raw` as uploadable. Those are treated
  as images when deciding whether a stored object can be resized, but they are
  not on the upload allowlist.

## [1.10.0] - 2026-08-03

### Security

- **Stored XSS through text uploads (`.csv`, `.sql`).** Content validation
  accepts a file whose bytes are valid UTF-8 text, which is how those formats get
  through, and `http.DetectContentType` reports `text/html` for anything opening
  with markup. Together they meant `<script>…</script>` uploaded as `evil.csv`
  came back with `Content-Type: text/html` on this service's own origin and
  executed for every visitor who opened the link. `X-Content-Type-Options:
  nosniff` was no defence: the browser was not guessing, it was being told.

  Sniffed `text/html`, `application/xhtml+xml`, `text/xml` and `application/xml`
  are now served as `text/plain`. The stored bytes are unchanged; only the type
  they are announced under is. `.html` is not on the extension allowlist, so a
  sniffed HTML type is never something a caller legitimately stored. SVG is
  unaffected, since it is declared as itself and defended by its sandbox CSP.

  Present since `.sql` was allowlisted, so this predates the `.csv` addition in
  this release. Two prior audits missed it because both reasoned about
  extensions rather than about what the bytes sniff as. `/resize` echoed uploads
  back the same way and is fixed alongside.

- **MinIO and Redis were published on every interface.** `docker-compose.yml`
  bound `9000`, `9001` and `6379` to `0.0.0.0`, putting an S3 endpoint holding
  every object, an admin console and a password-less Redis on the public
  internet of any host with a public IP. A host firewall does not cover this:
  Docker writes its own iptables rules and a published port bypasses `ufw`.

  All three are now bound to `127.0.0.1` (`MINIO_BIND` / `REDIS_BIND` to
  override). Nothing in the stack needed them published: the API replicas reach
  both over the compose network.

### Fixed

- **Production ran out of memory roughly seven times a day.** Three replicas on a
  12 GB host held 2.5-2.9 GB each while idle and were killed by the host OOM
  killer; `RestartCount` had reached 299. Docker reported `OOMKilled=false`
  throughout, because it only flags a cgroup-limit kill and this was host-wide, so
  the restarts looked spontaneous. Two causes, both now closed:
  - ImageMagick starts one OpenMP thread per core by default, and its pixel
    buffers come from `malloc` rather than the Go heap. glibc gives each thread
    its own arena and effectively never returns it to the OS, so resident memory
    only ratcheted upward. `IMAGICK_THREAD_LIMIT` (default 2) now caps the thread
    count in `ApplyImagickResourceLimits`, is exported as `MAGICK_THREAD_LIMIT`
    before ImageMagick's genesis (which is when the pool is built, so setting it
    afterwards is too late), and is repeated in `docker/policy.xml` for images run
    outside this project's boot path. `MALLOC_ARENA_MAX=2` in the dockerfile bounds
    the same growth from the allocator side. The measured effect on the affected
    deployment was 2.9 GB down to roughly 200 MB per replica, and 108 threads down
    to 13.
  - The `GET` resize path had no concurrency bound at all. `OPTIMIZE_MAX_CONCURRENT`
    guarded uploads only, so a gallery page requesting twenty thumbnails started
    twenty simultaneous full decodes, each holding a whole raster. `RESIZE_MAX_CONCURRENT`
    (default 4) now bounds them. A request that finds every slot busy waits up to
    `RESIZE_QUEUE_TIMEOUT_SEC`, and only then serves the original unresized,
    marked `Cache-Control: no-store` so that a momentary overload cannot leave a
    full-size image cached under a `?width=` URL for the cache's lifetime.
- **Archived objects were empty.** `S3PutObject` was handed the same reader the
  MinIO upload had just drained, with no rewind in between, so every object the
  archive received was zero bytes. Nothing surfaced it: the upload still reported
  success, and nothing ever read the archive back. Both single-file upload paths
  now rewind before archiving; `BatchUpload` already did.
- **Every cache miss was logged as an error.** `redis.Nil` means "key not
  present", which on this service is the common case: the rate limiter looks up a
  key per client IP and the first request from any IP misses by definition. The
  result was one `ERROR` line per rate-limited request, each recording a client
  IP, drowning the log. Misses are now counted separately from failures in both
  the logs and the `cache_operations` metric, where they had shared the `miss`
  label and made a Redis outage read as a cold cache.
- **The rate limiter's storage adapter violated fiber's contract.** `Storage.Get`
  must answer `(nil, nil)` for an absent key; it was returning an error, which is
  what produced the log flood above.
- **nginx asked the upstream to upgrade every connection.** `proxy_set_header
  Connection "upgrade"` was set unconditionally rather than from a `map` on
  `$http_upgrade`, so every ordinary `GET` announced a protocol switch and the
  `keepalive 32` pool was never used. Every request paid for a fresh TCP
  connection.
- **The image cache in `nginx.conf` did nothing, and has been removed rather than
  repaired.** `proxy_cache_valid` and `proxy_cache_use_stale` were configured
  without a `proxy_cache` zone, which silently disables both, so every request
  reached the Go service and `X-Cache-Status` was always empty. Adding the zone
  is the obvious fix and it is the wrong one: with caching at this layer a
  deleted object kept being served until its entry expired, and the open source
  nginx build has no purge, so `DELETE` had no way to take effect. That was
  caught by an existing test asserting deletion is immediate. Caching belongs in
  the CDN or proxy in front, where the operator already controls invalidation;
  the dead directives are gone so the config no longer describes behaviour it
  does not have.

### Added

- **Cold-storage archive with instant reads.** When AWS credentials are
  configured, every upload is mirrored to S3 and objects MinIO no longer holds are
  streamed straight from the archive, so a URL keeps working after its object has
  been aged out locally.
  - The storage class is now **Glacier Instant Retrieval** rather than Glacier
    Flexible Retrieval. Both cost about the same to store, but Flexible Retrieval
    cannot be read with a `GET` at all: it needs a `RestoreObject` call and
    minutes to hours before a temporary copy appears. That is unusable behind a
    CDN URL. Instant Retrieval answers a normal `GET` in milliseconds.
  - Objects are addressed by the same `(bucket, key)` pair as in MinIO, so nothing
    has to be indexed to find one again. `ARCHIVE_BUCKET` optionally collapses
    every bucket into one under a `<minio-bucket>/` prefix, which is easier to
    administer once there are more than a handful.
  - `ARCHIVE_ONLY_BUCKETS` narrows which buckets are archived; the default is all
    of them, so a newly created bucket is never silently left unprotected. It
    gates writing and not reading, so removing a bucket from the list cannot
    break URLs for objects archived while it was still included. Named to stay a
    clear distance from `ARCHIVE_BUCKET`, which means something else entirely.
  - Nothing is copied back into MinIO on a cold read. Re-warming would let a
    single crawl of old content refill the disk this exists to keep empty.
  - Worth knowing before tuning: AWS bills objects under 128 KB as 128 KB in this
    class, and bills a delete before 90 days as 90 days.
- **`POST /archive`, on-demand tiering.** An application names the objects it no
  longer needs served locally, and they are copied to the archive and removed
  from MinIO. The URL does not change: a read that misses locally is served from
  the archive, so links already sitting in a caller's database keep working.
  - Accepts object keys **or** full CDN URLs, since callers usually store the URL
    this service returned at upload time. A URL pointing at another host or
    another bucket is rejected rather than guessed at.
  - `evict: false` copies to the archive and keeps the local copy.
  - Idempotent: a second call on an already-archived object reports
    `already_archived` rather than an error, so batches are safe to retry.
  - Bucket-scoped tokens work and are confined to their own bucket.
  - This exists because age is not a usable signal everywhere. On a CDN that
    objects were migrated into, the stored timestamps describe the migration
    rather than the content, so a five-year-old document and yesterday's photo
    look the same age and no retention window can separate them. The application
    that owns the content is the only party that knows.
- **Word documents, CSV and ZIP are accepted** (`.doc`, `.docx`, `.csv`, `.zip`),
  with the matching MIME types. `.xlsx` and `.pptx` were already supported, so
  the absence of `.docx` was an oversight rather than a policy. The content gate
  needed nothing new: legacy Office files are OLE compound documents and the
  modern ones are ZIP containers, both of which it already recognised, and CSV
  passes through the UTF-8 text branch. All four are covered by round-trip tests.

  Note that the content check confirms a container, never what is inside it, and
  that a formula in a CSV cell is stored verbatim; the risk there belongs to
  whatever opens the file, not to serving it.
- **`backfill` command** (`cmd/backfill`, shipped in the image alongside the
  server). Copies objects that predate the archive into it, which is what makes
  retention and `/archive` able to free anything at all on an existing
  deployment: without an archived copy both correctly refuse to delete. It copies
  and never moves, has no delete call anywhere in it, and is safe to interrupt
  and rerun because each object is checked against the archive before transfer.
  Defaults to reporting; `-apply` performs the transfer.
- **`restore` command** (`cmd/restore`), `backfill` in the other direction: it
  walks what the archive holds and writes it back into local storage, creating a
  local bucket if it no longer exists. It ships because adopting cold storage
  must not be a one-way door. Like `backfill` it copies and never deletes:
  nothing is removed from the archive, and emptying the S3 bucket stays a
  deliberate act performed from AWS once local storage is confirmed complete.
  Ignores `ARCHIVE_ONLY_BUCKETS`, since pulling back a bucket that was dropped
  from the scope is one of the cases it exists for.
- **Retention job.** Deletes MinIO objects older than `RETENTION_DAYS`, and only
  those the archive is confirmed to hold at a matching size. It is off by default
  (`RETENTION_ENABLED=false`) and reports without deleting the first time it is
  switched on (`RETENTION_DRY_RUN=true`), because deleting files is the one
  irreversible thing this service does. It refuses to start without an archive,
  never deletes from the archive, and keeps (with a warning) anything it cannot
  verify. The size comparison is not redundant with an existence check: it is
  exactly what would have caught the zero-byte archive bug above.
- **Per-container memory limits** in `docker-compose.yml` (`API_MEM_LIMIT`,
  default 2500m). Without a limit the kernel's OOM killer works host-wide and
  picks the largest process, which is as likely to be MinIO, and every object it
  serves, as the replica that actually misbehaved.

### Changed

- **The retention sweep runs its per-object work concurrently**
  (`RETENTION_MAX_CONCURRENT`, default 8). Every eligible object costs one
  `HeadObject` against the archive before it can be deleted, and doing those one
  at a time makes a pass over a few million objects take days: not a slow sweep
  so much as one that never finishes. Listing stays sequential, since it is a
  cheap local stream, and the cap keeps a routine sweep from turning into a
  denial of service against the archive.
- **Archiving is no longer requested per upload.** The `aws_upload` form field and
  JSON property are accepted and ignored; whether a deployment archives is now a
  property of the deployment, on when AWS credentials are present and off when
  they are not. An opt-in flag was safe while MinIO kept everything forever, but
  it stops being safe once the retention job can remove local copies: a caller who
  forgot the flag would have left the only copy of an object on the disk being
  swept. The field stays in the request types so existing clients keep working
  rather than failing validation.
- **A missing or unreachable archive no longer fails an upload.** The
  `BucketExists` precondition on the archive side is gone from all three upload
  paths. The object belongs in MinIO whether or not cold storage is reachable, and
  refusing it would turn an archive misconfiguration into a total upload outage.
  A failed archive write is reported in the response and leaves the object
  ineligible for retention, which is the safe end.

### Fixed (tests)

- **`pkg/batch` tests raced with the code they were testing.** The processor
  callback runs on the BatchProcessor's goroutines, and every subtest read the
  variables it wrote without synchronisation, which `-race` flagged in five
  places. The concurrency-limit case was the worst: it measured peak concurrency
  with an unsynchronised counter, which is the one thing such a counter cannot
  do. Rewritten around a guarded recorder, and the fixed `time.Sleep` waits are
  replaced with bounded polling so a slow machine makes the suite slower rather
  than flaky.
- **Added coverage for non-image content**, which the suite had none of despite
  the service accepting documents, audio and video: byte-for-byte round trips for
  PDF, xlsx, pptx, mp4, mp3, wav and sql, a 5 MB object through the streaming
  read path, confirmation that resize parameters are ignored for non-images, and
  that extensions outside the allowlist are still refused.
- **Added coverage for batch upload**, asserting per file that each stored object
  holds the content of the file it was named for. Concurrency bugs in that path
  show up as one file's bytes under another file's name, which a count-only
  assertion cannot see.
- **Added coverage for `pkg/validator`, which had none.** All three upload gates
  are now pinned: the extension allowlist (including case handling and that a
  double extension is judged on its last one), the MIME list, and the content
  check. The content tests matter most, since that gate is the only one an
  attacker does not control: every supported format is confirmed to pass it, and
  an ELF header is confirmed to fail it whatever it is named.
- **Added a concurrency test for the retention sweep** covering 300 objects
  across all three verification outcomes, asserting that every verified object is
  deleted exactly once and no unverified one is touched regardless of interleaving.

### Fixed (previously unreleased)

- **Path-based resize served the original image.** `/{bucket}/w:{width}/h:{height}/{path}`
  and its width-only and height-only variants are routed and documented, but
  `GetImage` only ever read the query form, so all three quietly returned the
  image untouched. Introduced in `0b66ad1`, which replaced the path lookup with
  the query one instead of adding it; both are read now. The query form
  (`?width=&height=`) was never affected.
- **SVG objects could not be rendered by browsers.** They were served as
  `text/plain`, because `http.DetectContentType` cannot recognise SVG, and the
  global `X-Content-Type-Options: nosniff` then correctly stopped the browser
  from treating them as images: `<img src="…​.svg">` displayed nothing. They now
  go out as `image/svg+xml`. The protection against inline `<script>` was never
  the wrong content type, it is the `default-src 'none'; sandbox` CSP that was
  already being sent, and that is unchanged. Everything else keeps its sniffed
  type, so a valid image carrying an appended payload still serves as `image/*`.
- **The Kubernetes deployment could not reach MinIO.** `MINIO_ROOT_USER` and
  `MINIO_ROOT_PASSWORD` sat in `cdn-secrets` but were never wired into the
  container, as did the AWS credentials. `MINIO_USE_SSL`, the `DISABLE_*` flags
  and `APP_URL` were defined in the ConfigMap and equally unused, the last of
  which meant upload responses would hand callers a `cdn.example.com` link. All
  are now wired; the AWS ones are marked optional so a MinIO-only cluster can
  omit them entirely.

### Fixed (documentation)

- **The API reference misstated which endpoints need a credential.** A global
  `security` default meant every operation without an explicit override inherited
  `BearerAuth`, so the spec claimed a token was required for `GET /health` and for
  serving objects (`GET /{bucket}/{path}` and the resize variant), all three of
  which are public. `GET /ws` had the opposite problem: it declared `security: []`
  while actually requiring a token. All four now say what the router does, and
  `/ws` gets a `TokenQuery` scheme, since a browser cannot set an Authorization
  header on a WebSocket handshake.
- `public/scalar.yaml` was still versioned 1.8.0, and listed `localhost:9090`
  ahead of the production server, so the public documentation page defaulted to
  pointing readers at their own machine.

### Added

- **End-to-end tests for the behaviour that was previously only checked by hand**
  (`test/integration/image_test.go`): the upload, serve, resize and delete
  lifecycle, dimension clamping, rejection of files whose content does not match
  their extension, inert serving of a valid image carrying an appended payload,
  and bucket-scoped token isolation including the operator routes. They skip
  unless a stack is running and `CDN_TEST_TOKEN` is set, in the same spirit as
  the existing integration tests.

### Fixed (tests)

- `TestMetricsEndpoint` asserted that `/metrics` returns 200 without a token,
  which stopped being true in 1.7.0 when the endpoint was closed off. It had
  never failed because it skips when no server is running. It now checks both
  sides of the gate: rejected without a token, 200 with one.

## [1.9.0] - 2026-07-26

### Security

- **The general `TOKEN` is now checked for strength at boot** (#10). It was only
  checked for emptiness, so a one-character token or a copied placeholder started
  the service and was accepted on every authenticated endpoint, operator routes
  included. It must now be at least 32 characters, the same floor bucket-scoped
  tokens already had, and must not be a template placeholder. **Breaking** for a
  deployment running a short token: it will refuse to start until the token is
  replaced. `.env.example` now ships `REPLACE_ME`, which fails that check on
  purpose.
- **The image no longer ships a build toolchain** (#16, see below). A production
  image without compilers, headers or a source tree is a smaller thing to attack.

### Added

- **Audit logging on the authentication path** (#11). Rejected credentials and
  denied bucket access were previously invisible: a credential-stuffing run, a
  misconfigured client and a scoped token probing other tenants all produced an
  error response and no record. The new `pkg/audit` emits three events at `warn`,
  each carrying the method, path and the Cloudflare-aware trusted client IP:
  - `auth.failure` when a credential is rejected, with the reason
  - `auth.scoped_token_on_operator_route` when a *valid* bucket-scoped token
    reaches `/aws/*`, `/minio/*`, `/monitor` or `/metrics`, recorded separately
    because that is almost always a misconfigured client rather than an attack
  - `auth.bucket_access_denied` when a scoped token names another bucket, with
    both bucket names, which is what separates a misconfigured client from
    someone probing for other tenants

  No credential material is logged, in any form. Successful authentication is
  deliberately not logged: on a CDN that is the hot path and the access log
  already covers it.

- **Optional expiry for bucket-scoped tokens** (#13). An entry may carry an
  `expires_at` RFC 3339 timestamp; omitting it means the token never expires, so
  existing files are unaffected. Expiry is evaluated per request rather than at
  load time, which matters under the read-once-at-boot model: a token stops
  working the moment it expires instead of lasting until the next restart. An
  expired token is rejected exactly like an unknown one. A mistyped timestamp
  fails the boot rather than being read as "no expiry", and the boot log warns
  about entries that have already expired or expire within a week.
- **Bucket-scoped tokens can be deployed on Kubernetes** (#12). `k8s/secrets.yaml`
  gained a `tokens_json` key, projected by `k8s/deployment.yaml` into the pod at
  `/app/config/tokens.json` from a read-only, `optional` secret volume. It holds
  credentials, so it is a Secret and not a ConfigMap. While wiring it, `TOKEN`
  itself turned out never to have been wired into the deployment at all, which
  meant the manifests could not boot; it is now read from `cdn-secrets.app_token`.

### Changed

- **The image is built in two stages and is 9x smaller** (#16), from **1.88 GB**
  down to **205 MB**. The single-stage build shipped everything used to produce
  the binary: the Go toolchain (251 MB on its own), `build-essential`, the `-dev`
  packages, the ImageMagick source tree and the whole repository. The runtime
  stage now starts from `debian:bullseye-slim` (matching the build stage's glibc
  and codec libraries) and receives only the binary, `public/`, the ImageMagick
  shared libraries and modules, and the runtime library closure, which was
  derived with `ldd` over the binary and every coder module and mapped to
  packages with `dpkg -S`. `ca-certificates` is installed explicitly: it is
  absent from the slim base, and without it every outbound HTTPS call
  (`/upload-url`, the AWS SDK) would fail x509 verification.

  The hardened `policy.xml` and `MAGICK_CONFIGURE_PATH` carry over to the runtime
  stage. Because losing either would disable the ImageMagick hardening silently,
  CI now asserts the policy is live in the image that ships (`magick -list
  policy`) and that PNG, JPEG, WebP, GIF and TIFF still decode there while the
  PDF coder stays blocked. That last check matters because the Go tests run in
  the build stage, so a codec package missing from the runtime stage would not
  have failed them.
- **ImageMagick is pinned instead of tracking the newest upstream release**
  (#15). The dockerfile resolved "whatever is newest" at build time, so two
  builds of the same commit could ship different ImageMagick versions, and an
  upstream publish could break CI and deploys at once with no change here. The
  version and a SHA-256 checksum are now `ARG`s in the dockerfile, and the
  tarball comes from the GitHub release assets, which are immutable;
  `download.imagemagick.org/archive/releases` keeps only the newest release of
  each major line, so a pin against that host stops resolving as soon as the next
  version ships. Pinned at 7.1.2-27, the version that was being built before this
  change. Note that the archive host was serving a beta-labelled tarball of that
  version (`7.1.2-27 (Beta) ... 24344`); the release asset is the final one
  (`7.1.2-27 ... e4c2b403b`), so the image no longer ships a beta build.
  `scripts/get_imagemagick_version.sh` is no longer part of the build and is now
  an upgrade helper that prints the version and checksum in the format the
  dockerfile expects.
- **CI caches the image build** (#14). The ImageMagick compile dominated the job,
  putting every run at roughly seven minutes; the build now goes through buildx
  with the GitHub Actions cache backend, so the layer is reused until the pinned
  version changes.

## [1.8.0] - 2026-07-25

### Added

- **Bucket-scoped write tokens (bucket isolation).** A credential can now be
  limited to a single bucket instead of the whole service. Wire format is
  `Authorization: Bearer <bucket>:<token>`; entries live in `config/tokens.json`
  (template: `config/tokens.template.json`, path overridable via `TOKENS_FILE`)
  and are read once at boot.
  - Accepted on `/upload`, `/batch/upload`, `/upload-url`, `/batch/delete`,
    `DELETE /{bucket}/{path}` and `/resize`. The `bucket` form/body field becomes
    optional for these callers because the token's bucket is authoritative;
    naming a different bucket returns **403**.
  - Secrets are SHA-256 hashed in memory after load, compared in constant time,
    and never logged. An unknown bucket costs the same as a wrong secret.
  - The bucket prefix is attacker-controlled and grants nothing on its own: the
    secret is only ever compared against the entry for that exact bucket.
  - `config/tokens.json` is git-ignored and docker-ignored, mounted read-only at
    runtime (`./config:/app/config:ro`) and never baked into an image layer.
  - A missing token file is not an error: deployments that use only the general
    `TOKEN` behave exactly as before. A file that exists but is invalid (bad JSON,
    token under 32 characters, invalid or duplicate bucket name) stops boot rather
    than silently serving with missing credentials.
  - No runtime token management endpoints: with three API replicas behind nginx,
    file writes and revocations would not propagate. Edit the file and restart.

### Changed / Hardening

- **Rate-limit bypass fixed, and the raw token is out of the Redis keyspace.**
  `RateLimitKey` built the key as `<ip>:<raw bearer>`, which had two problems.
  The bearer secret was written verbatim into Redis, and because the key trusted
  an unverified header, a client sending a different random token on every
  request got a brand-new counter each time and was never rate limited at all.
  The key is now derived from the *verified* identity of the credential:
  `<ip>|general`, `<ip>|bucket:<name>`, or the plain `<ip>` for anything that does
  not authenticate. A verified credential still gets its own quota, unverified
  ones now share the per-IP bucket they were always meant to be under, and no
  token material reaches Redis. Bucket names are validated at load time, so they
  cannot inject a separator into the key.
- **Operator endpoints now require the general token.** `AuthMiddleware` was split
  into `GeneralAuthMiddleware` (general token only, applied to `/aws/*`,
  `/minio/*`, `/monitor`, `/metrics`) and `BucketAuthMiddleware` (accepts both
  token kinds, applied to the write endpoints). Without this a bucket-scoped token
  could have called `DELETE /minio/{bucket}/delete` on any bucket or started
  cost-bearing Glacier retrievals. `/ws` already required the general token.
- **Bucket names are validated on creation.** Upload paths that implicitly create
  a missing bucket now require 3-63 characters of lowercase letters, digits or
  `-` (new `pkg/bucket`). Previously any string reaching `/upload` or
  `/upload-url` could create an arbitrarily named bucket. Existing buckets are not
  re-validated, so nothing that works today stops working.
- Documentation: `public/scalar.yaml` bumped to 1.8.0 with both token formats, a
  per-tag statement of which token each group needs, the `403` response on the
  write endpoints, and `bucket` dropped from the required fields of
  `/upload-url` and `/batch/delete`. README gained an Authentication section.

### Backward compatibility

The general `TOKEN` continues to work on every endpoint it works on today, and a
general token containing a `:` is still matched as a whole string before any
`<bucket>:<secret>` parsing is attempted (covered by a regression test).
Auth failures keep returning 400 rather than being corrected to 401, so client
error handling is unchanged. The only new status code is the 403 for a
bucket-scoped token reaching outside its bucket.

The rate-limit key format changed, so counters in flight at deploy time are
orphaned and expire on their own within the rate-limit window. No action needed.

## [1.7.2] - 2026-07-25

### Security

- **Hardened the ImageMagick attack surface (RCE prevention).** ImageMagick was
  the only server-side code-execution surface (uploads are stored in MinIO and
  streamed back, never executed). Three gaps closed:
  - `POST /resize` was unauthenticated and fed request bytes straight into
    ImageMagick; it is now behind `AuthMiddleware` like every other write/compute
    endpoint.
  - `ResizeImage` now runs the same magic-number content check
    (`ValidateFileContent`) as the upload endpoints before any decode.
  - Added a strict ImageMagick `policy.xml` (`docker/policy.xml`, loaded via
    `MAGICK_CONFIGURE_PATH`) disabling the Ghostscript delegate chain
    (PS/EPS/PDF/XPS), the ImageTragick coder class
    (MSL/MVG/MSVG/SVG/MAGICK/SHOW/URL/HTTPS/HTTP/FTP/EPHEMERAL/TEXT), indirect
    file reads (`path @*`) and all delegates. Raster decode (JPEG/PNG/GIF/WEBP)
    and SVG upload/serving are unaffected.

## [1.7.1] - 2026-07-21

### Fixed

- **Upload to a non-existent bucket now creates it.** `BucketExists` returns
  `(false, nil)` for a missing bucket, so the previous `err != nil && !exists`
  guard never created it and the upload failed with "bucket does not exist".
  Fixed in `/upload` and `/upload-url` (create whenever the bucket is missing).
- **`DELETE /batch/delete` now reaches `BatchDelete`.** It was shadowed by the
  `DELETE /:bucket/*` wildcard (matched as bucket="batch", \*="delete") and
  routed to the single-object delete handler; the route is now registered before
  the wildcard.

## [1.7.0] - 2026-07-21

> Releases 1.4.0 through 1.6.x are not itemized here; see the GitHub Releases
> page for their notes.

### Changed / Hardening

- **Runtime robustness:** global panic-recover middleware (any handler panic → 500, no process crash); fixed a nil-index panic on uploads without a Content-Type part; hardened all Glacier AWS-SDK pointer derefs (`aws.ToString`/`aws.ToInt64` + nil guards) and added a nil `Body` guard + close (also fixes a reader leak); fixed a data race on async Glacier job state and bounded the in-memory job map.
- **Uploads:** a file with an image extension must now be a valid image (raster decoded via ImageMagick, SVG validated structurally); non-image files pass through unchanged. Honors `VALIDATE_FILE`.
- **Monitoring auth:** `/metrics` now requires a Bearer token; `/ws` requires a `token` query parameter (previously both were open).
- **Rate limiter:** retries Redis at boot and fails open (serves without rate limiting) instead of panicking the process on a Redis outage.
- **Config/secrets:** `.env` is injected via docker-compose `env_file` and excluded from the image (`.dockerignore`); `godotenv` load is now non-fatal (missing security envs are still caught by the TOKEN fail-fast). Removed a startup log that dumped the MinIO client (credential material). `MINIO_USE_SSL` is now honored.
- **DoS limits:** batch endpoints capped at `MAX_BATCH_FILES` (default 100); per-connection header buffer bounded via `READ_BUFFER_SIZE_KB` (default 1 MB, was 24 MB); GET/DELETE reject `..` path segments in object keys.

### Added

- Opt-in upload optimization: `optimize=true` on `/upload`, `/batch/upload` and
  `/upload-url` stores a visually-lossless, size-reduced image (re-encode +
  metadata strip + longest-side cap `OPTIMIZE_MAX_DIMENSION`, default 2560px).
  Animated GIFs and non-images pass through untouched; on any failure the
  original is stored so uploads never fail because of optimization.
- Process-wide ImageMagick resource limits (`IMAGICK_MEMORY_LIMIT_MB`,
  `IMAGICK_MAP_LIMIT_MB`, `IMAGICK_AREA_LIMIT_MP`, `IMAGICK_DISK_LIMIT_MB`,
  `IMAGICK_TIME_LIMIT_SEC`, `IMAGICK_WIDTH_LIMIT`, `IMAGICK_HEIGHT_LIMIT`) to
  bound decode cost and defend against decompression bombs.

### Security

- **Auth bypass fixed (breaking):** the server now fails fast on boot when
  `TOKEN` is empty, and token comparison is constant-time. Previously an empty
  server token let an empty client token pass authentication.
- **Path traversal fixed:** the Glacier async local-download target is now
  contained within `/tmp/glacier_downloads`; absolute/empty targets are
  rejected before any file is created.
- **SSRF fixed:** `/upload-url` now rejects non-http(s) schemes and
  private/loopback/link-local (incl. cloud-metadata) targets, validated both at
  request time and at dial time (DNS-rebinding safe). Opt out with
  `UPLOAD_URL_ALLOW_PRIVATE=true` for internal setups.
- **Resize DoS hardened:** on-the-fly resize width/height is clamped to
  `MAX_RESIZE_DIMENSION` (default 4096) and negative values are floored, closing
  a uint-wraparound giant-allocation path on the public GET route.
- **MIME hardening:** `X-Content-Type-Options: nosniff` on all responses and a
  restrictive `Content-Security-Policy` (sandbox) on served `.svg` objects to
  neutralize stored-XSS via SVG.

## [1.3.0] - 2024-01-15

### Added

- Secure file validation using magic bytes for enhanced security
- Improved image processing with optimized resize operations
- Redis cache integration for better performance
- AWS S3 Glacier storage class support
- Worker pool implementation for concurrent processing
- Health check endpoint with detailed status
- Prometheus metrics integration
- Circuit breaker pattern implementation:
  - Automatic failure detection and recovery
  - Configurable thresholds and timeouts
  - State monitoring via Prometheus metrics
  - Integration with AWS and storage services
- New batch operations endpoints:
  - `/batch/upload` for multiple file uploads
  - `/batch/delete` for multiple file deletions
- AWS operations made optional via `aws_upload` parameter
- Real-time monitoring via WebSocket
  - System metrics (CPU, memory, disk usage)
  - Active uploads tracking
  - Cache hit rate monitoring
  - Upload speed statistics
  - Error logs streaming
- REST endpoint for current system stats
- Batch operations for multiple file uploads/deletions

### Changed

- Refactored image processing service for better reliability
- Enhanced error handling with detailed messages
- Updated logging system with structured logs
- Improved request validation
- Optimized cache invalidation strategy
- AWS S3 operations now controlled by request parameters
- Standardized response format for batch operations

### Fixed

- Memory leak in image processing operations
- Concurrent upload handling issues
- Cache invalidation race conditions
- File type validation security issues
- Error handling in batch operations
- AWS upload parameter handling

## [1.2.0] - 2023-12-01

### Added

- Basic image processing capabilities
- MinIO storage integration
- Simple caching mechanism
- Basic error handling
- Initial API endpoints

### Changed

- Improved file upload process
- Enhanced storage handling
- Basic performance optimizations

## [1.1.0] - 2023-09-15

### Added

- Initial MinIO integration
- Basic file upload/download
- Simple authentication

## [1.0.0] - 2023-06-15

- Initial release
- Basic CDN functionality
