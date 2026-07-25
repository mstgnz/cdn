#!/bin/bash
#
# Manual helper for upgrading the pinned ImageMagick version. NOT used by the
# build: the dockerfile pins IMAGEMAGICK_VERSION and IMAGEMAGICK_SHA256 so that
# builds are reproducible and an upstream release cannot change what we ship
# without a commit here.
#
# Prints the newest release and the checksum of its source tarball, formatted so
# that both lines can be pasted straight into the dockerfile.
#
# Usage:
#   ./scripts/get_imagemagick_version.sh            # newest release
#   ./scripts/get_imagemagick_version.sh 7.1.2-27   # a specific release

set -euo pipefail

VERSION="${1:-}"

if [ -z "$VERSION" ]; then
  VERSION=$(curl -sSL --fail https://api.github.com/repos/ImageMagick/ImageMagick/releases/latest |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
fi

if [ -z "$VERSION" ]; then
  echo "could not determine the ImageMagick version" >&2
  exit 1
fi

TARBALL="ImageMagick-${VERSION}.tar.xz"
URL="https://github.com/ImageMagick/ImageMagick/releases/download/${VERSION}/${TARBALL}"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

echo "downloading ${URL}" >&2
curl -sSL --fail -o "${TMP_DIR}/${TARBALL}" "$URL"

if command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM=$(sha256sum "${TMP_DIR}/${TARBALL}" | cut -d' ' -f1)
else
  # macOS has shasum instead
  CHECKSUM=$(shasum -a 256 "${TMP_DIR}/${TARBALL}" | cut -d' ' -f1)
fi

echo "ARG IMAGEMAGICK_VERSION=${VERSION}"
echo "ARG IMAGEMAGICK_SHA256=${CHECKSUM}"
