# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/main.go

# Final stage
FROM debian:bullseye-slim

# Copy version check script
COPY scripts/get_imagemagick_version.sh /tmp/
RUN chmod +x /tmp/get_imagemagick_version.sh

# Set environment variables
ENV DEBIAN_FRONTEND=noninteractive
ENV IMAGEMAGICK_VERSION=$(/tmp/get_imagemagick_version.sh)

# Install dependencies and ImageMagick
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    build-essential \
    pkg-config \
    libpng-dev \
    libjpeg-dev \
    libtiff-dev \
    libwebp-dev \
    && cd /tmp \
    && wget https://download.imagemagick.org/archive/releases/ImageMagick-${IMAGEMAGICK_VERSION}.tar.gz \
    && tar xvzf ImageMagick-${IMAGEMAGICK_VERSION}.tar.gz \
    && cd ImageMagick-* \
    && ./configure \
    && make \
    && make install \
    && ldconfig /usr/local/lib \
    && apt-get remove -y build-essential wget \
    && apt-get autoremove -y \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && rm -rf /tmp/*

WORKDIR /app
COPY --from=builder /app/main .
COPY --from=builder /app/public ./public

EXPOSE 9090
CMD ["./main"]
