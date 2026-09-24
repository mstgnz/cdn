#!/bin/sh
# Replays .github/workflows/go.yml on this machine, plus an ldd sweep of the
# runtime image. Run with `make ci`. Needs only Docker.
set -eu
cd "$(dirname "$0")/.."

step() { printf '\n### %s\n' "$*"; }

step "build stage"
docker build -q --target build -t cdn:ci-build -f dockerfile . >/dev/null
docker run --rm --entrypoint go cdn:ci-build version

step "vet + test"
# Per-package coverage is printed on each "ok" line; the total closes the step.
docker run --rm --entrypoint sh cdn:ci-build -c '
  go vet ./... &&
  go test -count=1 -coverprofile=/tmp/cover.out ./... &&
  go tool cover -func=/tmp/cover.out | tail -1'

step "runtime image"
docker build -q -t cdn:ci -f dockerfile . >/dev/null
docker image ls cdn:ci --format 'size {{.Size}}'

step "ImageMagick policy"
policy=$(docker run --rm --entrypoint magick cdn:ci -list policy)
for pattern in "Path: /etc/ImageMagick/policy.xml" "pattern: PDF" "pattern: MSL" "Policy: Delegate"; do
  echo "$policy" | grep -q -- "$pattern" || { echo "policy is not active, missing: $pattern"; exit 1; }
done
echo "policy active"

step "decode in the runtime image"
docker run --rm --entrypoint sh cdn:ci -c '
  set -e
  for fmt in png jpeg webp gif tiff; do
    magick -size 64x64 gradient:red-blue "/tmp/t.$fmt"
    magick "/tmp/t.$fmt" -resize 32x32 "info:-" >/dev/null
    echo "$fmt ok"
  done
  if magick -size 10x10 xc:white /tmp/x.pdf 2>/dev/null; then
    echo "PDF coder is not blocked"; exit 1
  fi
  echo "pdf blocked"'

# The decode check proves the coders work; this proves nothing the binaries link
# against is missing, which is what a stale runtime package list breaks.
step "unresolved libraries in the runtime image"
docker run --rm --entrypoint sh cdn:ci -c '
  for f in /app/main /app/backfill /app/restore /usr/local/bin/magick /usr/local/lib/libMagick*.so*; do ldd "$f"; done \
    | grep "not found" && exit 1 || echo "none"'

step "hostwatch image"
docker build -q -f docker/hostwatch.dockerfile -t cdn-hostwatch:ci . >/dev/null
docker image ls cdn-hostwatch:ci --format 'size {{.Size}}'

printf '\nci: all steps passed\n'
