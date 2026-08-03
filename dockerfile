# Stage 1: compile ImageMagick 7 and the service binary.
#
# The Go binding is cgo, so this stage needs a full toolchain: compilers, the -dev
# headers and the ImageMagick source tree. None of that belongs in the image that
# runs in production, which is what the second stage is for.
FROM golang:1.22-bullseye AS build

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies and ImageMagick
RUN apt-get update && apt-get install -y \
    git \
    gcc \
    curl \
    wget \
    build-essential \
    pkg-config \
    xz-utils \
    libpng-dev \
    libjpeg-dev \
    libtiff-dev \
    libwebp-dev \
    libmagickwand-dev \
    libmagickcore-dev \
    imagemagick \
    && ldconfig

# ImageMagick is pinned instead of tracking whatever is newest upstream. Building
# "the latest release" made two builds of the same commit produce different
# binaries, and let an upstream publish break CI and deploys at once with no
# change here.
#
# The tarball comes from the GitHub release assets because those are immutable.
# download.imagemagick.org/archive/releases keeps only the newest release of each
# major line, so a pin against that host stops resolving as soon as the next
# version ships.
#
# To upgrade, bump both values together in their own commit.
# `scripts/get_imagemagick_version.sh` prints the newest version and its checksum
# in exactly the format these two lines expect.
ARG IMAGEMAGICK_VERSION=7.1.2-27
ARG IMAGEMAGICK_SHA256=f65773f1f465c730f1c025c92a2524966f772ebeb77de0d817c336e1f565e9ef

RUN cd /tmp && \
    wget -q "https://github.com/ImageMagick/ImageMagick/releases/download/${IMAGEMAGICK_VERSION}/ImageMagick-${IMAGEMAGICK_VERSION}.tar.xz" && \
    echo "${IMAGEMAGICK_SHA256}  ImageMagick-${IMAGEMAGICK_VERSION}.tar.xz" | sha256sum -c - && \
    tar xJf "ImageMagick-${IMAGEMAGICK_VERSION}.tar.xz" && \
    cd "ImageMagick-${IMAGEMAGICK_VERSION}" && \
    ./configure && \
    make && \
    make install && \
    ldconfig /usr/local/lib && \
    cd / && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /tmp/*

# Gather the ImageMagick pieces the runtime actually needs into one tree.
#
# Copying /usr/local wholesale is not an option: it holds /usr/local/go, 251 MB of
# Go toolchain. Naming the module directory explicitly is not either, since it
# carries the version (ImageMagick-7.1.2) and would drift from the ARG above. So
# the glob happens here, where a shell is available, and the runtime stage copies
# a fixed path. Shared objects only: the static archives are build-time artifacts.
RUN mkdir -p /runtime/lib /runtime/etc /runtime/share /runtime/bin && \
    cp -a /usr/local/lib/libMagick*.so* /runtime/lib/ && \
    cp -a /usr/local/lib/ImageMagick-* /runtime/lib/ && \
    cp -a /usr/local/etc/ImageMagick-* /runtime/etc/ && \
    cp -a /usr/local/share/ImageMagick-* /runtime/share/ && \
    cp -a /usr/local/bin/magick /runtime/bin/

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o main ./cmd/main.go

# The backfill tool ships alongside the server rather than as its own image: it
# links the same service package, so it needs the same ImageMagick runtime, and
# an operator running it wants it inside the deployment it is backfilling.
RUN CGO_ENABLED=1 GOOS=linux go build -o backfill ./cmd/backfill

# restore is backfill's inverse, and it ships for the same reason a fire exit
# does: adopting cold storage must not be a one-way door.
RUN CGO_ENABLED=1 GOOS=linux go build -o restore ./cmd/restore

# Stage 2: the image that ships.
#
# bullseye-slim and not a newer Debian on purpose: the binary and the ImageMagick
# libraries are linked against bullseye's glibc and codec libraries, so the
# runtime has to be the same release.
FROM debian:bullseye-slim

ENV DEBIAN_FRONTEND=noninteractive

# The runtime library closure, derived by running `ldd` over the binary, the
# Magick libraries and every coder module in the build stage, then mapping the
# results to packages with `dpkg -S`. Regenerate the same way if the ImageMagick
# version or its configure-detected delegates change.
#
# ca-certificates is not part of that closure but is required all the same: it is
# absent from the slim base, and without it every outbound HTTPS call
# (/upload-url fetches, the AWS SDK) fails x509 verification.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libbrotli1 \
    libbsd0 \
    libbz2-1.0 \
    libdeflate0 \
    libdjvulibre21 \
    libexpat1 \
    libfontconfig1 \
    libfreetype6 \
    libgcc-s1 \
    libglib2.0-0 \
    libgomp1 \
    libicu67 \
    libilmbase25 \
    libjbig0 \
    libjpeg62-turbo \
    liblcms2-2 \
    liblqr-1-0 \
    liblzma5 \
    libmd0 \
    libopenexr25 \
    libopenjp2-7 \
    libpcre3 \
    libpng16-16 \
    libstdc++6 \
    libtiff5 \
    libuuid1 \
    libwebp6 \
    libwebpdemux2 \
    libwebpmux3 \
    libx11-6 \
    libxau6 \
    libxcb1 \
    libxdmcp6 \
    libxext6 \
    libxml2 \
    libzstd1 \
    zlib1g \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /runtime/lib/ /usr/local/lib/
COPY --from=build /runtime/etc/ /usr/local/etc/
COPY --from=build /runtime/share/ /usr/local/share/
# The `magick` CLI is kept so that an operator can confirm the security policy is
# live in a running container (`magick -list policy`). The service never shells
# out to it.
COPY --from=build /runtime/bin/magick /usr/local/bin/magick
RUN ldconfig /usr/local/lib

# Install the hardened ImageMagick security policy. MAGICK_CONFIGURE_PATH makes
# it authoritative over the configuration installed by the source build, closing
# the Ghostscript-delegate / ImageTragick RCE classes on untrusted uploads.
# Verify with `magick -list policy` after changing anything in this stage: losing
# this file or the variable disables the policy silently.
COPY docker/policy.xml /etc/ImageMagick/policy.xml
ENV MAGICK_CONFIGURE_PATH=/etc/ImageMagick

# Bound the number of glibc malloc arenas. ImageMagick allocates its pixel
# buffers through malloc rather than the Go heap, and glibc gives each thread its
# own arena which it then holds on to: freed buffers stay charged to the process
# and RSS only ever climbs. Left at the default, three replicas on an 8-core host
# grew to 2.5-2.9 GB each at idle and were repeatedly OOM-killed. Set at the image
# level so a plain `docker run` of this image is protected too, not only the
# compose deployment.
ENV MALLOC_ARENA_MAX=2

WORKDIR /app
COPY --from=build /app/main ./main
COPY --from=build /app/backfill ./backfill
COPY --from=build /app/restore ./restore
COPY --from=build /app/public ./public

EXPOSE 9090
ENTRYPOINT ["./main"]
