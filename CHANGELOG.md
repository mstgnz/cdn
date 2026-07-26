# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Fixed

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
