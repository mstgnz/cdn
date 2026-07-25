FROM golang:1.22-bullseye

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
    apt-get remove -y wget && \
    apt-get autoremove -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /tmp/*

# Install the hardened ImageMagick security policy. MAGICK_CONFIGURE_PATH makes
# it authoritative regardless of the ImageMagick major version or install prefix
# (source-built IM7 under /usr/local vs apt IM6 under /etc), closing the
# Ghostscript-delegate / ImageTragick RCE classes on untrusted uploads.
COPY docker/policy.xml /etc/ImageMagick/policy.xml
ENV MAGICK_CONFIGURE_PATH=/etc/ImageMagick

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o main ./cmd/main.go

EXPOSE 9090
ENTRYPOINT ["./main"]
