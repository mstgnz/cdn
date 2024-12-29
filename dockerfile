# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev pkgconfig imagemagick-dev

WORKDIR /build
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o app ./cmd/main.go

# Final stage
FROM debian:bullseye-slim

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies and ImageMagick
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    build-essential \
    pkg-config \
    libpng-dev \
    libjpeg-dev \
    libtiff-dev \
    libwebp-dev

# Copy and run version check script
COPY scripts/get_imagemagick_version.sh /tmp/
RUN chmod +x /tmp/get_imagemagick_version.sh && \
    cd /tmp && \
    VERSION=$(/tmp/get_imagemagick_version.sh) && \
    wget https://download.imagemagick.org/archive/releases/ImageMagick-${VERSION}.tar.gz && \
    tar xvzf ImageMagick-${VERSION}.tar.gz && \
    cd ImageMagick-* && \
    ./configure && \
    make && \
    make install && \
    ldconfig /usr/local/lib && \
    cd / && \
    apt-get remove -y build-essential wget && \
    apt-get autoremove -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /tmp/*

WORKDIR /app
COPY --from=builder /build/app /app/main
COPY --from=builder /build/public /app/public

EXPOSE 9090
ENTRYPOINT ["/app/main"]
